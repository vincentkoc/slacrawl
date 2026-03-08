package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
pragma foreign_keys = on;
pragma journal_mode = wal;
pragma busy_timeout = 5000;

create table if not exists workspaces (
  id text primary key,
  name text not null,
  domain text,
  enterprise_id text,
  raw_json text not null,
  updated_at text not null
);

create table if not exists channels (
  id text primary key,
  workspace_id text not null,
  name text not null,
  kind text not null,
  topic text,
  purpose text,
  is_private integer not null default 0,
  is_archived integer not null default 0,
  is_shared integer not null default 0,
  is_general integer not null default 0,
  raw_json text not null,
  updated_at text not null
);

create table if not exists users (
  id text primary key,
  workspace_id text not null,
  name text not null,
  real_name text,
  display_name text,
  title text,
  is_bot integer not null default 0,
  is_deleted integer not null default 0,
  raw_json text not null,
  updated_at text not null
);

create table if not exists messages (
  channel_id text not null,
  ts text not null,
  workspace_id text not null,
  user_id text,
  subtype text,
  client_msg_id text,
  thread_ts text,
  parent_user_id text,
  text text not null,
  normalized_text text not null,
  reply_count integer not null default 0,
  latest_reply text,
  edited_ts text,
  deleted_ts text,
  source_rank integer not null,
  source_name text not null,
  raw_json text not null,
  updated_at text not null,
  primary key (channel_id, ts)
);

create table if not exists message_events (
  id integer primary key autoincrement,
  channel_id text not null,
  ts text not null,
  event_type text not null,
  source_name text not null,
  payload_json text not null,
  created_at text not null
);

create table if not exists sync_state (
  source_name text not null,
  entity_type text not null,
  entity_id text not null,
  value text not null,
  updated_at text not null,
  primary key (source_name, entity_type, entity_id)
);

create table if not exists message_mentions (
  channel_id text not null,
  ts text not null,
  mention_type text not null,
  target_id text not null,
  display_text text,
  primary key (channel_id, ts, mention_type, target_id)
);

create table if not exists embedding_jobs (
  id integer primary key autoincrement,
  channel_id text not null,
  ts text not null,
  state text not null,
  created_at text not null
);

create virtual table if not exists message_fts using fts5(message_key unindexed, content);
`

type Store struct {
	db *sql.DB
}

type Workspace struct {
	ID           string
	Name         string
	Domain       string
	EnterpriseID string
	RawJSON      string
	UpdatedAt    time.Time
}

type Channel struct {
	ID          string
	WorkspaceID string
	Name        string
	Kind        string
	Topic       string
	Purpose     string
	IsPrivate   bool
	IsArchived  bool
	IsShared    bool
	IsGeneral   bool
	RawJSON     string
	UpdatedAt   time.Time
}

type User struct {
	ID          string
	WorkspaceID string
	Name        string
	RealName    string
	DisplayName string
	Title       string
	IsBot       bool
	IsDeleted   bool
	RawJSON     string
	UpdatedAt   time.Time
}

type Message struct {
	ChannelID      string
	TS             string
	WorkspaceID    string
	UserID         string
	Subtype        string
	ClientMsgID    string
	ThreadTS       string
	ParentUserID   string
	Text           string
	NormalizedText string
	ReplyCount     int
	LatestReply    string
	EditedTS       string
	DeletedTS      string
	SourceRank     int
	SourceName     string
	RawJSON        string
	UpdatedAt      time.Time
}

type Mention struct {
	Type        string
	TargetID    string
	DisplayText string
}

type Status struct {
	Workspaces  int       `json:"workspaces"`
	Channels    int       `json:"channels"`
	Users       int       `json:"users"`
	Messages    int       `json:"messages"`
	LastSyncAt  time.Time `json:"last_sync_at"`
	ThreadState string    `json:"thread_state"`
}

type MessageRow struct {
	ChannelID      string `json:"channel_id"`
	TS             string `json:"ts"`
	UserID         string `json:"user_id"`
	Text           string `json:"text"`
	NormalizedText string `json:"normalized_text"`
	ThreadTS       string `json:"thread_ts"`
	Subtype        string `json:"subtype"`
}

type MentionRow struct {
	ChannelID   string `json:"channel_id"`
	TS          string `json:"ts"`
	MentionType string `json:"mention_type"`
	TargetID    string `json:"target_id"`
	DisplayText string `json:"display_text"`
}

type UserRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RealName    string `json:"real_name"`
	DisplayName string `json:"display_name"`
	Title       string `json:"title"`
}

type ChannelRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type ChannelSyncCursor struct {
	ID       string
	LatestTS string
}

type SyncStateRow struct {
	SourceName string `json:"source_name"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Value      string `json:"value"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) UpsertWorkspace(ctx context.Context, workspace Workspace) error {
	_, err := s.db.ExecContext(ctx, `
insert into workspaces (id, name, domain, enterprise_id, raw_json, updated_at)
values (?, ?, ?, ?, ?, ?)
on conflict(id) do update set
  name=excluded.name,
  domain=excluded.domain,
  enterprise_id=excluded.enterprise_id,
  raw_json=excluded.raw_json,
  updated_at=excluded.updated_at
`, workspace.ID, workspace.Name, workspace.Domain, workspace.EnterpriseID, workspace.RawJSON, workspace.UpdatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) UpsertChannel(ctx context.Context, channel Channel) error {
	_, err := s.db.ExecContext(ctx, `
insert into channels (id, workspace_id, name, kind, topic, purpose, is_private, is_archived, is_shared, is_general, raw_json, updated_at)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(id) do update set
  workspace_id=excluded.workspace_id,
  name=excluded.name,
  kind=excluded.kind,
  topic=excluded.topic,
  purpose=excluded.purpose,
  is_private=excluded.is_private,
  is_archived=excluded.is_archived,
  is_shared=excluded.is_shared,
  is_general=excluded.is_general,
  raw_json=excluded.raw_json,
  updated_at=excluded.updated_at
`, channel.ID, channel.WorkspaceID, channel.Name, channel.Kind, channel.Topic, channel.Purpose, boolInt(channel.IsPrivate), boolInt(channel.IsArchived), boolInt(channel.IsShared), boolInt(channel.IsGeneral), channel.RawJSON, channel.UpdatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) UpsertUser(ctx context.Context, user User) error {
	_, err := s.db.ExecContext(ctx, `
insert into users (id, workspace_id, name, real_name, display_name, title, is_bot, is_deleted, raw_json, updated_at)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(id) do update set
  workspace_id=excluded.workspace_id,
  name=excluded.name,
  real_name=excluded.real_name,
  display_name=excluded.display_name,
  title=excluded.title,
  is_bot=excluded.is_bot,
  is_deleted=excluded.is_deleted,
  raw_json=excluded.raw_json,
  updated_at=excluded.updated_at
`, user.ID, user.WorkspaceID, user.Name, user.RealName, user.DisplayName, user.Title, boolInt(user.IsBot), boolInt(user.IsDeleted), user.RawJSON, user.UpdatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) UpsertMessage(ctx context.Context, message Message, mentions []Mention) error {
	key := messageKey(message.ChannelID, message.TS)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
insert into messages (
  channel_id, ts, workspace_id, user_id, subtype, client_msg_id, thread_ts, parent_user_id,
  text, normalized_text, reply_count, latest_reply, edited_ts, deleted_ts, source_rank,
  source_name, raw_json, updated_at
) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(channel_id, ts) do update set
  workspace_id=excluded.workspace_id,
  user_id=excluded.user_id,
  subtype=excluded.subtype,
  client_msg_id=excluded.client_msg_id,
  thread_ts=excluded.thread_ts,
  parent_user_id=excluded.parent_user_id,
  text=excluded.text,
  normalized_text=excluded.normalized_text,
  reply_count=excluded.reply_count,
  latest_reply=excluded.latest_reply,
  edited_ts=excluded.edited_ts,
  deleted_ts=excluded.deleted_ts,
  source_rank=case
    when excluded.source_rank <= messages.source_rank then excluded.source_rank
    else messages.source_rank
  end,
  source_name=case
    when excluded.source_rank <= messages.source_rank then excluded.source_name
    else messages.source_name
  end,
  raw_json=case
    when excluded.source_rank <= messages.source_rank then excluded.raw_json
    else messages.raw_json
  end,
  updated_at=excluded.updated_at
`, message.ChannelID, message.TS, message.WorkspaceID, message.UserID, message.Subtype, message.ClientMsgID, message.ThreadTS, message.ParentUserID, message.Text, message.NormalizedText, message.ReplyCount, message.LatestReply, message.EditedTS, message.DeletedTS, message.SourceRank, message.SourceName, message.RawJSON, message.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `delete from message_mentions where channel_id = ? and ts = ?`, message.ChannelID, message.TS)
	if err != nil {
		return err
	}
	seenMentions := map[string]struct{}{}
	for _, mention := range mentions {
		key := mention.Type + "|" + mention.TargetID + "|" + mention.DisplayText
		if _, ok := seenMentions[key]; ok {
			continue
		}
		seenMentions[key] = struct{}{}
		if _, err := tx.ExecContext(ctx, `
insert into message_mentions (channel_id, ts, mention_type, target_id, display_text)
values (?, ?, ?, ?, ?)
on conflict(channel_id, ts, mention_type, target_id) do update set
  display_text=excluded.display_text
`, message.ChannelID, message.TS, mention.Type, mention.TargetID, mention.DisplayText); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `delete from message_fts where message_key = ?`, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `insert into message_fts (message_key, content) values (?, ?)`, key, message.NormalizedText); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
insert into message_events (channel_id, ts, event_type, source_name, payload_json, created_at)
values (?, ?, ?, ?, ?, ?)
`, message.ChannelID, message.TS, eventType(message), message.SourceName, message.RawJSON, message.UpdatedAt.Format(time.RFC3339)); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) SetSyncState(ctx context.Context, source, entityType, entityID, value string) error {
	_, err := s.db.ExecContext(ctx, `
insert into sync_state (source_name, entity_type, entity_id, value, updated_at)
values (?, ?, ?, ?, ?)
on conflict(source_name, entity_type, entity_id) do update set
  value=excluded.value,
  updated_at=excluded.updated_at
`, source, entityType, entityID, value, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) Status(ctx context.Context) (Status, error) {
	status := Status{}
	for query, target := range map[string]*int{
		`select count(*) from workspaces`: &status.Workspaces,
		`select count(*) from channels`:   &status.Channels,
		`select count(*) from users`:      &status.Users,
		`select count(*) from messages`:   &status.Messages,
	} {
		if err := s.db.QueryRowContext(ctx, query).Scan(target); err != nil {
			return Status{}, err
		}
	}

	var lastSync sql.NullString
	if err := s.db.QueryRowContext(ctx, `select max(updated_at) from sync_state where source_name != 'doctor'`).Scan(&lastSync); err != nil {
		return Status{}, err
	}
	if lastSync.Valid {
		parsed, err := time.Parse(time.RFC3339, lastSync.String)
		if err == nil {
			status.LastSyncAt = parsed
		}
	}

	status.ThreadState = "partial"
	var hasUser sql.NullString
	if err := s.db.QueryRowContext(ctx, `select value from sync_state where source_name = 'doctor' and entity_type = 'threads' and entity_id = 'coverage'`).Scan(&hasUser); err == nil {
		status.ThreadState = hasUser.String
	}

	return status, nil
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]MessageRow, error) {
	rows, err := s.db.QueryContext(ctx, `
select m.channel_id, m.ts, m.user_id, m.text, m.normalized_text, m.thread_ts, m.subtype
from message_fts f
join messages m on f.message_key = m.channel_id || '|' || m.ts
where message_fts match ?
order by m.ts desc
limit ?
`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessageRows(rows)
}

func (s *Store) Messages(ctx context.Context, channelID string, userID string, limit int) ([]MessageRow, error) {
	query := `
select channel_id, ts, user_id, text, normalized_text, thread_ts, subtype
from messages
where 1=1`
	args := []any{}
	if channelID != "" {
		query += ` and channel_id = ?`
		args = append(args, channelID)
	}
	if userID != "" {
		query += ` and user_id = ?`
		args = append(args, userID)
	}
	query += ` order by ts desc limit ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessageRows(rows)
}

func (s *Store) Mentions(ctx context.Context, target string, limit int) ([]MentionRow, error) {
	query := `
select channel_id, ts, mention_type, target_id, coalesce(display_text, '')
from message_mentions
where 1=1`
	args := []any{}
	if target != "" {
		query += ` and (target_id = ? or display_text like ?)`
		args = append(args, target, "%"+target+"%")
	}
	query += ` order by ts desc limit ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MentionRow
	for rows.Next() {
		var row MentionRow
		if err := rows.Scan(&row.ChannelID, &row.TS, &row.MentionType, &row.TargetID, &row.DisplayText); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) QueryReadOnly(ctx context.Context, query string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(strings.ToLower(query))
	if !strings.HasPrefix(trimmed, "select") && !strings.HasPrefix(trimmed, "with") {
		return nil, errors.New("only read-only select statements are allowed")
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, col := range cols {
			row[col] = stringifyDBValue(values[i])
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) Users(ctx context.Context, query string, limit int) ([]UserRow, error) {
	rows, err := s.db.QueryContext(ctx, `
select id, name, coalesce(real_name, ''), coalesce(display_name, ''), coalesce(title, '')
from users
where (? = '' or id = ? or name like ? or real_name like ? or display_name like ?)
order by name asc
limit ?
`, query, query, "%"+query+"%", "%"+query+"%", "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var row UserRow
		if err := rows.Scan(&row.ID, &row.Name, &row.RealName, &row.DisplayName, &row.Title); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) Channels(ctx context.Context, query string, limit int) ([]ChannelRow, error) {
	rows, err := s.db.QueryContext(ctx, `
select id, name, kind
from channels
where (? = '' or id = ? or name like ?)
order by name asc
limit ?
`, query, query, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelRow
	for rows.Next() {
		var row ChannelRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Kind); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) ChannelSyncCursors(ctx context.Context, workspaceID string) ([]ChannelSyncCursor, error) {
	rows, err := s.db.QueryContext(ctx, `
select c.id, coalesce(max(case when m.ts not like 'draft:%' then m.ts end), '')
from channels c
left join messages m on m.channel_id = c.id and m.workspace_id = c.workspace_id
where c.workspace_id = ?
group by c.id
order by c.id asc
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChannelSyncCursor
	for rows.Next() {
		var row ChannelSyncCursor
		if err := rows.Scan(&row.ID, &row.LatestTS); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) RenameChannel(ctx context.Context, channelID string, name string) error {
	_, err := s.db.ExecContext(ctx, `
update channels
set name = ?, updated_at = ?
where id = ?
`, name, time.Now().UTC().Format(time.RFC3339), channelID)
	return err
}

func (s *Store) SetChannelArchived(ctx context.Context, channelID string, archived bool) error {
	_, err := s.db.ExecContext(ctx, `
update channels
set is_archived = ?, updated_at = ?
where id = ?
`, boolInt(archived), time.Now().UTC().Format(time.RFC3339), channelID)
	return err
}

func (s *Store) GetSyncState(ctx context.Context, source, entityType, entityID string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `
select value from sync_state
where source_name = ? and entity_type = ? and entity_id = ?
`, source, entityType, entityID).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (s *Store) ListSyncState(ctx context.Context, source, entityType string, limit int) ([]SyncStateRow, error) {
	rows, err := s.db.QueryContext(ctx, `
select source_name, entity_type, entity_id, value
from sync_state
where (? = '' or source_name = ?)
  and (? = '' or entity_type = ?)
order by updated_at desc, entity_id asc
limit ?
`, source, source, entityType, entityType, RequireLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SyncStateRow
	for rows.Next() {
		var row SyncStateRow
		if err := rows.Scan(&row.SourceName, &row.EntityType, &row.EntityID, &row.Value); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func MarshalRaw(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func scanMessageRows(rows *sql.Rows) ([]MessageRow, error) {
	var out []MessageRow
	for rows.Next() {
		var row MessageRow
		if err := rows.Scan(&row.ChannelID, &row.TS, &row.UserID, &row.Text, &row.NormalizedText, &row.ThreadTS, &row.Subtype); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func eventType(message Message) string {
	switch {
	case message.DeletedTS != "":
		return "message_deleted"
	case message.EditedTS != "":
		return "message_changed"
	default:
		return "message"
	}
}

func messageKey(channelID string, ts string) string {
	return channelID + "|" + ts
}

func stringifyDBValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	default:
		return typed
	}
}

func DebugJSON(value any) string {
	data, _ := json.MarshalIndent(value, "", "  ")
	return string(data)
}

func ParseTime(value string) string {
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Format(time.RFC3339)
	}
	return value
}

func RequireLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}

func PrettyStatus(status Status) string {
	last := "never"
	if !status.LastSyncAt.IsZero() {
		last = status.LastSyncAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("workspaces=%d channels=%d users=%d messages=%d last_sync=%s thread_state=%s",
		status.Workspaces, status.Channels, status.Users, status.Messages, last, status.ThreadState)
}
