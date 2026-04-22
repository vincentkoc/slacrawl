package report

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vincentkoc/slacrawl/internal/store"
)

type Options struct {
	Now time.Time
}

type ActivityReport struct {
	GeneratedAt     time.Time     `json:"generated_at"`
	LatestMessageAt time.Time     `json:"latest_message_at"`
	TotalWorkspaces int           `json:"total_workspaces"`
	TotalChannels   int           `json:"total_channels"`
	TotalUsers      int           `json:"total_users"`
	TotalMessages   int           `json:"total_messages"`
	DraftMessages   int           `json:"draft_messages"`
	EditedMessages  int           `json:"edited_messages"`
	DeletedMessages int           `json:"deleted_messages"`
	Windows         []WindowStats `json:"windows"`
	TopChannels     []RankedCount `json:"top_channels"`
	TopAuthors      []RankedCount `json:"top_authors"`
	BusiestDays     []RankedCount `json:"busiest_days"`
}

type WindowStats struct {
	Label          string    `json:"label"`
	Since          time.Time `json:"since"`
	Messages       int       `json:"messages"`
	ActiveAuthors  int       `json:"active_authors"`
	ActiveChannels int       `json:"active_channels"`
}

type RankedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func Build(ctx context.Context, s *store.Store, opts Options) (ActivityReport, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	report := ActivityReport{GeneratedAt: now.UTC()}
	if err := scanTotals(ctx, s.DB(), &report); err != nil {
		return ActivityReport{}, err
	}
	anchor := report.LatestMessageAt
	if anchor.IsZero() {
		anchor = now.UTC()
	}
	windows := []struct {
		label string
		dur   time.Duration
	}{
		{label: "24 hours", dur: 24 * time.Hour},
		{label: "7 days", dur: 7 * 24 * time.Hour},
		{label: "30 days", dur: 30 * 24 * time.Hour},
	}
	for _, window := range windows {
		stats, err := scanWindow(ctx, s.DB(), window.label, anchor.Add(-window.dur))
		if err != nil {
			return ActivityReport{}, err
		}
		report.Windows = append(report.Windows, stats)
	}
	var err error
	report.TopChannels, err = topChannels(ctx, s.DB(), anchor.Add(-7*24*time.Hour), 8)
	if err != nil {
		return ActivityReport{}, err
	}
	report.TopAuthors, err = topAuthors(ctx, s.DB(), anchor.Add(-7*24*time.Hour), 8)
	if err != nil {
		return ActivityReport{}, err
	}
	report.BusiestDays, err = busiestDays(ctx, s.DB(), anchor.Add(-30*24*time.Hour), 7)
	if err != nil {
		return ActivityReport{}, err
	}
	return report, nil
}

func scanTotals(ctx context.Context, db *sql.DB, report *ActivityReport) error {
	var latestEpoch sql.NullInt64
	if err := db.QueryRowContext(ctx, `
select
	(select count(*) from workspaces),
	(select count(*) from channels),
	(select count(*) from users),
	(select count(*) from messages),
	(select count(*) from messages where ts like 'draft:%'),
	(select count(*) from messages where edited_ts != ''),
	(select count(*) from messages where deleted_ts != ''),
	(
		select max(
			case
				when ts like 'draft:%' then null
				when instr(ts, '.') > 0 then cast(substr(ts, 1, instr(ts, '.') - 1) as integer)
				else null
			end
		)
		from messages
	)
`).Scan(
		&report.TotalWorkspaces,
		&report.TotalChannels,
		&report.TotalUsers,
		&report.TotalMessages,
		&report.DraftMessages,
		&report.EditedMessages,
		&report.DeletedMessages,
		&latestEpoch,
	); err != nil {
		return fmt.Errorf("scan report totals: %w", err)
	}
	if latestEpoch.Valid {
		report.LatestMessageAt = time.Unix(latestEpoch.Int64, 0).UTC()
	}
	return nil
}

func scanWindow(ctx context.Context, db *sql.DB, label string, since time.Time) (WindowStats, error) {
	stats := WindowStats{Label: label, Since: since.UTC()}
	cutoff := since.UTC().Unix()
	if err := db.QueryRowContext(ctx, `
select
	count(*),
	count(distinct nullif(user_id, '')),
	count(distinct nullif(channel_id, ''))
from messages
where ts not like 'draft:%'
  and instr(ts, '.') > 0
  and cast(substr(ts, 1, instr(ts, '.') - 1) as integer) >= ?
`, cutoff).Scan(&stats.Messages, &stats.ActiveAuthors, &stats.ActiveChannels); err != nil {
		return WindowStats{}, fmt.Errorf("scan %s stats: %w", label, err)
	}
	return stats, nil
}

func topChannels(ctx context.Context, db *sql.DB, since time.Time, limit int) ([]RankedCount, error) {
	return ranked(ctx, db, `
select coalesce(nullif(c.name, ''), m.channel_id) as name, count(*) as total
from messages m
left join channels c on c.id = m.channel_id and c.workspace_id = m.workspace_id
where m.ts not like 'draft:%'
  and instr(m.ts, '.') > 0
  and cast(substr(m.ts, 1, instr(m.ts, '.') - 1) as integer) >= ?
group by m.workspace_id, m.channel_id, coalesce(nullif(c.name, ''), m.channel_id)
order by total desc, name asc
limit ?
`, since.UTC().Unix(), limit)
}

func topAuthors(ctx context.Context, db *sql.DB, since time.Time, limit int) ([]RankedCount, error) {
	return ranked(ctx, db, `
select
	coalesce(
		nullif(u.display_name, ''),
		nullif(u.real_name, ''),
		nullif(u.name, ''),
		nullif(m.user_id, ''),
		'unknown'
	) as name,
	count(*) as total
from messages m
left join users u on u.id = m.user_id and u.workspace_id = m.workspace_id
where m.ts not like 'draft:%'
  and instr(m.ts, '.') > 0
  and cast(substr(m.ts, 1, instr(m.ts, '.') - 1) as integer) >= ?
group by m.workspace_id, m.user_id, name
order by total desc, name asc
limit ?
`, since.UTC().Unix(), limit)
}

func busiestDays(ctx context.Context, db *sql.DB, since time.Time, limit int) ([]RankedCount, error) {
	return ranked(ctx, db, `
select strftime('%Y-%m-%d', cast(substr(ts, 1, instr(ts, '.') - 1) as integer), 'unixepoch') as name, count(*) as total
from messages
where ts not like 'draft:%'
  and instr(ts, '.') > 0
  and cast(substr(ts, 1, instr(ts, '.') - 1) as integer) >= ?
group by strftime('%Y-%m-%d', cast(substr(ts, 1, instr(ts, '.') - 1) as integer), 'unixepoch')
order by total desc, name desc
limit ?
`, since.UTC().Unix(), limit)
}

func ranked(ctx context.Context, db *sql.DB, query string, args ...any) ([]RankedCount, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RankedCount
	for rows.Next() {
		var row RankedCount
		if err := rows.Scan(&row.Name, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
