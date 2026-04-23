package report

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vincentkoc/slacrawl/internal/store"
)

const secondsPerWeek = int64(7 * 24 * 60 * 60)

// TrendsOptions controls how a Trends report is built.
type TrendsOptions struct {
	Now         time.Time
	Weeks       int
	WorkspaceID string
	Channel     string
}

// Trends summarizes week-over-week message volume per channel.
type Trends struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Since       time.Time   `json:"since"`
	Until       time.Time   `json:"until"`
	Weeks       int         `json:"weeks"`
	Workspace   string      `json:"workspace,omitempty"`
	Channel     string      `json:"channel,omitempty"`
	Rows        []TrendsRow `json:"rows"`
}

// TrendsRow is one channel's weekly message trend.
type TrendsRow struct {
	WorkspaceID string        `json:"workspace_id"`
	ChannelID   string        `json:"channel_id"`
	ChannelName string        `json:"channel_name"`
	Kind        string        `json:"kind,omitempty"`
	Weekly      []WeeklyCount `json:"weekly"`
}

// WeeklyCount is the message count for a specific week bucket.
type WeeklyCount struct {
	WeekStart time.Time `json:"week_start"`
	Messages  int       `json:"messages"`
}

// BuildTrends computes weekly message counts per channel.
func BuildTrends(ctx context.Context, s *store.Store, opts TrendsOptions) (Trends, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	weeks := opts.Weeks
	if weeks <= 0 {
		weeks = 8
	}

	currentWeekStart := startOfWeekUTC(now)
	since := currentWeekStart.AddDate(0, 0, -7*(weeks-1))

	rows, err := trendsRows(ctx, s.DB(), since, now, weeks, opts.WorkspaceID, opts.Channel)
	if err != nil {
		return Trends{}, err
	}

	return Trends{
		GeneratedAt: now,
		Since:       since,
		Until:       now,
		Weeks:       weeks,
		Workspace:   opts.WorkspaceID,
		Channel:     opts.Channel,
		Rows:        rows,
	}, nil
}

type trendKey struct {
	workspaceID string
	channelID   string
	channelName string
	kind        string
}

func trendsRows(ctx context.Context, db *sql.DB, since time.Time, until time.Time, weeks int, workspaceID string, channel string) ([]TrendsRow, error) {
	query := &strings.Builder{}
	query.WriteString(`
select
	m.workspace_id,
	m.channel_id,
	coalesce(nullif(c.name, ''), m.channel_id) as channel_name,
	coalesce(c.kind, '') as kind,
	cast(substr(m.ts, 1, instr(m.ts, '.') - 1) as integer) as ts_epoch
from messages m
left join channels c on c.id = m.channel_id and c.workspace_id = m.workspace_id
where m.ts not like 'draft:%'
  and instr(m.ts, '.') > 0
  and cast(substr(m.ts, 1, instr(m.ts, '.') - 1) as integer) >= ?
  and cast(substr(m.ts, 1, instr(m.ts, '.') - 1) as integer) <= ?
`)
	args := []any{since.Unix(), until.Unix()}
	if workspaceID != "" {
		query.WriteString("  and m.workspace_id = ?\n")
		args = append(args, workspaceID)
	}
	if channel != "" {
		query.WriteString("  and (m.channel_id = ? or c.name = ?)\n")
		args = append(args, channel, channel)
	}
	query.WriteString("order by channel_name asc, ts_epoch asc\n")

	rows, err := db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("trends rows query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	countsByChannel := make(map[trendKey]map[int64]int)
	for rows.Next() {
		var (
			key     trendKey
			tsEpoch int64
		)
		if err := rows.Scan(&key.workspaceID, &key.channelID, &key.channelName, &key.kind, &tsEpoch); err != nil {
			return nil, fmt.Errorf("trends rows scan: %w", err)
		}
		if countsByChannel[key] == nil {
			countsByChannel[key] = make(map[int64]int)
		}
		weekStart := startOfWeekUTC(time.Unix(tsEpoch, 0).UTC())
		countsByChannel[key][weekStart.Unix()]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if strings.TrimSpace(channel) != "" {
		matching, err := trendChannels(ctx, db, workspaceID, channel)
		if err != nil {
			return nil, err
		}
		for _, key := range matching {
			if countsByChannel[key] == nil {
				countsByChannel[key] = make(map[int64]int)
			}
		}
	}

	keys := make([]trendKey, 0, len(countsByChannel))
	for key := range countsByChannel {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].channelName != keys[j].channelName {
			return keys[i].channelName < keys[j].channelName
		}
		if keys[i].workspaceID != keys[j].workspaceID {
			return keys[i].workspaceID < keys[j].workspaceID
		}
		return keys[i].channelID < keys[j].channelID
	})

	out := make([]TrendsRow, 0, len(keys))
	for _, key := range keys {
		weekly := make([]WeeklyCount, 0, weeks)
		for i := 0; i < weeks; i++ {
			weekStart := since.AddDate(0, 0, 7*i)
			weekly = append(weekly, WeeklyCount{
				WeekStart: weekStart,
				Messages:  countsByChannel[key][weekStart.Unix()],
			})
		}
		out = append(out, TrendsRow{
			WorkspaceID: key.workspaceID,
			ChannelID:   key.channelID,
			ChannelName: key.channelName,
			Kind:        key.kind,
			Weekly:      weekly,
		})
	}
	return out, nil
}

func trendChannels(ctx context.Context, db *sql.DB, workspaceID string, channel string) ([]trendKey, error) {
	query := &strings.Builder{}
	query.WriteString(`
select
	workspace_id,
	id,
	coalesce(nullif(name, ''), id) as channel_name,
	coalesce(kind, '') as kind
from channels
where (id = ? or name = ?)
`)
	args := []any{channel, channel}
	if workspaceID != "" {
		query.WriteString("  and workspace_id = ?\n")
		args = append(args, workspaceID)
	}
	query.WriteString("order by channel_name asc, workspace_id asc, id asc\n")

	rows, err := db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("trends channels query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []trendKey
	for rows.Next() {
		var key trendKey
		if err := rows.Scan(&key.workspaceID, &key.channelID, &key.channelName, &key.kind); err != nil {
			return nil, fmt.Errorf("trends channels scan: %w", err)
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func startOfWeekUTC(t time.Time) time.Time {
	t = t.UTC()
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(dayStart.Weekday()) + 6) % 7
	return dayStart.AddDate(0, 0, -offset)
}
