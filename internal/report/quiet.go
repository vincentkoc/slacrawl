package report

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/vincentkoc/slacrawl/internal/store"
)

// QuietOptions controls how a Quiet report is built.
type QuietOptions struct {
	Now         time.Time
	Since       time.Duration
	WorkspaceID string
}

// Quiet summarizes channels with no activity in a window.
type Quiet struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Since       time.Time      `json:"since"`
	Until       time.Time      `json:"until"`
	Workspace   string         `json:"workspace,omitempty"`
	Channels    []QuietChannel `json:"channels"`
	Totals      QuietTotals    `json:"totals"`
}

// QuietChannel is one channel that has no recent activity.
type QuietChannel struct {
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Kind        string `json:"kind,omitempty"`
	WorkspaceID string `json:"workspace_id"`
	LastMessage string `json:"last_message,omitempty"`
	DaysSilent  int    `json:"days_silent"`
}

// QuietTotals summarizes quiet-channel counts.
type QuietTotals struct {
	Channels int `json:"channels"`
}

// BuildQuiet computes channels with no messages newer than the lookback window.
func BuildQuiet(ctx context.Context, s *store.Store, opts QuietOptions) (Quiet, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	sinceWindow := opts.Since
	if sinceWindow < 0 {
		sinceWindow = -sinceWindow
	}
	if sinceWindow == 0 {
		sinceWindow = 30 * 24 * time.Hour
	}
	since := now.Add(-sinceWindow)

	out := Quiet{
		GeneratedAt: now,
		Since:       since,
		Until:       now,
		Workspace:   opts.WorkspaceID,
	}

	channels, err := quietChannels(ctx, s.DB(), since, now, opts.WorkspaceID)
	if err != nil {
		return Quiet{}, err
	}
	out.Channels = channels
	out.Totals = QuietTotals{Channels: len(channels)}
	return out, nil
}

func quietChannels(ctx context.Context, db *sql.DB, since time.Time, now time.Time, workspaceID string) ([]QuietChannel, error) {
	query := &strings.Builder{}
	query.WriteString(`
with latest_messages as (
	select
		m.workspace_id,
		m.channel_id,
		max(cast(substr(m.ts, 1, instr(m.ts, '.') - 1) as integer)) as last_ts
	from messages m
	where m.ts not like 'draft:%'
	  and instr(m.ts, '.') > 0
	group by m.workspace_id, m.channel_id
)
select
	c.workspace_id,
	c.id as channel_id,
	coalesce(nullif(c.name, ''), c.id) as channel_name,
	coalesce(c.kind, '') as kind,
	lm.last_ts
from channels c
left join latest_messages lm on lm.workspace_id = c.workspace_id and lm.channel_id = c.id
where 1 = 1
`)
	args := make([]any, 0, 2)
	if workspaceID != "" {
		query.WriteString("  and c.workspace_id = ?\n")
		args = append(args, workspaceID)
	}
	query.WriteString("  and (lm.last_ts is null or lm.last_ts < ?)\n")
	args = append(args, since.Unix())
	query.WriteString(`order by
	case when lm.last_ts is null then 0 else 1 end,
	lm.last_ts asc,
	channel_name asc
`)

	rows, err := db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("quiet channels query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	minimumDaysSilent := int(now.Sub(since).Hours() / 24)
	out := make([]QuietChannel, 0)
	for rows.Next() {
		var row QuietChannel
		var lastEpoch sql.NullInt64
		if err := rows.Scan(&row.WorkspaceID, &row.ChannelID, &row.ChannelName, &row.Kind, &lastEpoch); err != nil {
			return nil, fmt.Errorf("quiet channels scan: %w", err)
		}
		if lastEpoch.Valid {
			last := time.Unix(lastEpoch.Int64, 0).UTC()
			row.LastMessage = last.Format(time.RFC3339)
			row.DaysSilent = int(now.Sub(last).Hours() / 24)
		} else {
			row.DaysSilent = minimumDaysSilent
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
