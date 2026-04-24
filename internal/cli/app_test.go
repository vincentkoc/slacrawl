package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vincentkoc/slacrawl/internal/config"
	"github.com/vincentkoc/slacrawl/internal/store"
)

func TestParseLookback(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"0d", 0, false},
		{"72h", 72 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-2d", 0, true},
		{"-1h", 0, true},
	}
	for _, c := range cases {
		d, err := parseLookback(c.in)
		if c.err {
			require.Error(t, err, "input=%q", c.in)
			continue
		}
		require.NoError(t, err, "input=%q", c.in)
		require.Equal(t, c.want, d, "input=%q", c.in)
	}
}

func TestMergeStringSlicesDedupesCaseInsensitive(t *testing.T) {
	got := mergeStringSlices(
		[]string{"general", " Ops-Alerts "},
		[]string{"#GENERAL", "random", "ops-alerts", ""},
	)
	require.Equal(t, []string{"general", "Ops-Alerts", "random"}, got)
}

func TestDigestCommandJSON(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	var stdout bytes.Buffer
	app := &App{Stdout: &stdout, Stderr: &stdout}
	ctx := context.Background()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "init", "--db", dbPath}))

	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()
	now := time.Now().UTC()
	makeTS := func(offset time.Duration, micros int) string {
		return fmt.Sprintf("%d.%06d", now.Add(-offset).Unix(), micros)
	}
	require.NoError(t, st.UpsertWorkspace(ctx, store.Workspace{ID: "T1", Name: "team", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C1", WorkspaceID: "T1", Name: "engineering", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertUser(ctx, store.User{ID: "U1", WorkspaceID: "T1", Name: "alice", DisplayName: "Alice", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID: "C1", TS: makeTS(1*time.Hour, 100), WorkspaceID: "T1", UserID: "U1",
		Text: "hello", NormalizedText: "hello", SourceRank: 2, SourceName: "api-bot", RawJSON: "{}",
		UpdatedAt: now,
	}, nil))
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID: "C1", TS: makeTS(2*time.Hour, 200), WorkspaceID: "T1", UserID: "U1",
		Text: "world", NormalizedText: "world", SourceRank: 2, SourceName: "api-bot", RawJSON: "{}",
		UpdatedAt: now,
	}, nil))

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "--json", "digest", "--since", "7d"}))
	var digest map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &digest))
	require.Equal(t, "7d", digest["window_label"])
	require.Equal(t, float64(1), digest["top_n"])
	totals, ok := digest["totals"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(2), totals["messages"])
	require.Equal(t, float64(1), totals["channels"])
	channels, ok := digest["channels"].([]any)
	require.True(t, ok)
	require.Len(t, channels, 1)
	row := channels[0].(map[string]any)
	require.Equal(t, "engineering", row["channel_name"])
	require.Equal(t, float64(2), row["messages"])
}

func TestInitStatusAndSQL(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	ctx := context.Background()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "init", "--db", dbPath}))

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "--json", "status"}))
	var status map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &status))
	require.Equal(t, float64(0), status["messages"])

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "--json", "sql", "select count(*) as messages from messages"}))
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.Equal(t, float64(0), rows[0]["messages"])

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "--format", "json", "status"}))
	var statusByFormat map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &statusByFormat))
	require.Equal(t, float64(0), statusByFormat["messages"])
}

func TestDoctorReflectsDisabledSources(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	cfg := config.Default()
	cfg.DBPath = dbPath
	cfg.Slack.Bot.Enabled = false
	cfg.Slack.App.Enabled = false
	cfg.Slack.User.Enabled = false
	cfg.Slack.Desktop.Enabled = false
	cfg.Slack.Desktop.Path = ""
	require.NoError(t, cfg.Save(configPath))

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	require.NoError(t, app.Run(context.Background(), []string{"--config", configPath, "--json", "doctor"}))

	var report map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	tokens := report["tokens"].(map[string]any)
	require.Equal(t, false, tokens["bot_enabled"])
	require.Equal(t, false, tokens["app_enabled"])
	require.Equal(t, false, tokens["user_enabled"])
	require.Equal(t, false, tokens["bot_set"])
	require.Equal(t, false, report["desktop_source"].(map[string]any)["available"])
}

func TestWatchFailsWhenDesktopDisabled(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	cfg := config.Default()
	cfg.DBPath = dbPath
	cfg.Slack.Desktop.Enabled = false
	cfg.Slack.Desktop.Path = ""
	require.NoError(t, cfg.Save(configPath))

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	err := app.Run(context.Background(), []string{"--config", configPath, "watch", "--desktop-every", "1s"})
	require.ErrorContains(t, err, "desktop sync is disabled in config")
}

func TestDoctorIncludesOperationalSyncState(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	cfg := config.Default()
	cfg.DBPath = dbPath
	cfg.Slack.Bot.Enabled = false
	cfg.Slack.App.Enabled = false
	cfg.Slack.User.Enabled = false
	cfg.Slack.Desktop.Enabled = false
	require.NoError(t, cfg.Save(configPath))

	st, err := store.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, st.SetSyncState(context.Background(), "api-bot", "channel_skip", "C111", "not_in_channel"))
	require.NoError(t, st.SetSyncState(context.Background(), "tail", "connection", "T123", "2026-03-08T18:20:43Z"))
	require.NoError(t, st.Close())

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	require.NoError(t, app.Run(context.Background(), []string{"--config", configPath, "--json", "doctor"}))

	var report map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	skips := report["api_channel_skips"].([]any)
	require.Len(t, skips, 1)
	skip := skips[0].(map[string]any)
	require.Equal(t, "C111", skip["entity_id"])
	require.Equal(t, "not_in_channel", skip["value"])

	tail := report["tail_state"].([]any)
	require.Len(t, tail, 1)
	state := tail[0].(map[string]any)
	require.Equal(t, "connection", state["entity_type"])
	require.Equal(t, "T123", state["entity_id"])
	shareState := report["share"].(map[string]any)
	require.Equal(t, false, shareState["enabled"])
}

func TestWorkspaceFilteredReadCommands(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	cfg := config.Default()
	cfg.DBPath = dbPath
	cfg.WorkspaceID = ""
	require.NoError(t, cfg.Save(configPath))

	st, err := store.Open(dbPath)
	require.NoError(t, err)
	now := mustTime(t, "2026-03-08T18:20:43Z")
	require.NoError(t, st.UpsertWorkspace(context.Background(), store.Workspace{ID: "T1", Name: "one", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertWorkspace(context.Background(), store.Workspace{ID: "T2", Name: "two", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(context.Background(), store.Channel{ID: "C1", WorkspaceID: "T1", Name: "alpha", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(context.Background(), store.Channel{ID: "C2", WorkspaceID: "T2", Name: "beta", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertUser(context.Background(), store.User{ID: "U1", WorkspaceID: "T1", Name: "alice", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertUser(context.Background(), store.User{ID: "U2", WorkspaceID: "T2", Name: "bob", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ChannelID:      "C1",
		TS:             "1.0",
		WorkspaceID:    "T1",
		UserID:         "U1",
		Text:           "incident alpha",
		NormalizedText: "incident alpha",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, []store.Mention{{Type: "user", TargetID: "U1", DisplayText: "alice"}}))
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ChannelID:      "C2",
		TS:             "2.0",
		WorkspaceID:    "T2",
		UserID:         "U2",
		Text:           "incident beta",
		NormalizedText: "incident beta",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, []store.Mention{{Type: "user", TargetID: "U2", DisplayText: "bob"}}))
	require.NoError(t, st.Close())

	var stdout bytes.Buffer
	app := &App{Stdout: &stdout, Stderr: &stdout}

	require.NoError(t, app.Run(context.Background(), []string{"--config", configPath, "--json", "search", "--workspace", "T2", "incident"}))
	var searchRows []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &searchRows))
	require.Len(t, searchRows, 1)
	require.Equal(t, "T2", searchRows[0]["workspace_id"])

	stdout.Reset()
	require.NoError(t, app.Run(context.Background(), []string{"--config", configPath, "--json", "channels", "--workspace", "T1"}))
	var channels []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &channels))
	require.Len(t, channels, 1)
	require.Equal(t, "T1", channels[0]["workspace_id"])

	stdout.Reset()
	require.NoError(t, app.Run(context.Background(), []string{"--config", configPath, "--json", "channels", "--workspace", "T1", "--kind", "public_channel"}))
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &channels))
	require.Len(t, channels, 1)

	stdout.Reset()
	require.NoError(t, app.Run(context.Background(), []string{"--config", configPath, "--json", "channels", "--workspace", "T1", "--kind", "public"}))
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &channels))
	require.Len(t, channels, 1)

	stdout.Reset()
	err = app.Run(context.Background(), []string{"--config", configPath, "--json", "channels", "--workspace", "T1", "--kind", "unknown"})
	require.ErrorContains(t, err, "invalid channel kind")
}

func TestHelpIncludesBannerAndUsage(t *testing.T) {
	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	require.NoError(t, app.Run(context.Background(), nil))

	out := stdout.String()
	require.Contains(t, out, "local-first slack mirror for SQLite")
	require.Contains(t, out, "Usage:")
	require.Contains(t, out, "slacrawl [global flags] <command> [args]")
	require.Contains(t, out, "--format <kind>")
	require.Contains(t, out, "--no-color")
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed
}

func TestStatusHumanOutputIsStructured(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	ctx := context.Background()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "init", "--db", dbPath}))

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "status"}))

	out := stdout.String()
	require.Contains(t, out, "STATUS")
	require.Contains(t, out, "workspaces")
	require.Contains(t, out, "messages")
	require.Contains(t, out, "Git share")
	require.True(t, strings.Contains(out, "never") || strings.Contains(out, "last sync"))
	require.NotContains(t, out, "map[]")
	require.NotContains(t, out, "\x1b[")
}

func TestDoctorHumanOutputSkipsEmptyShareTimes(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	ctx := context.Background()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "init", "--db", dbPath}))

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "doctor"}))

	out := stdout.String()
	require.Contains(t, out, "Git share")
	require.Contains(t, out, "not configured")
	require.NotContains(t, out, "map[]")
}

func TestStatusLogOutputIsLineOriented(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	ctx := context.Background()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "init", "--db", dbPath}))

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "--format", "log", "status"}))

	out := stdout.String()
	require.Contains(t, out, "status ")
	require.Contains(t, out, "messages=\"0\"")
	require.NotContains(t, out, "STATUS")
	require.NotContains(t, out, "local-first slack mirror for SQLite")
}

func TestInvalidFormatFails(t *testing.T) {
	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	err := app.Run(context.Background(), []string{"--format", "yaml", "status"})
	require.ErrorContains(t, err, "unsupported format")
}

func TestNoColorFlagDisablesANSIOnTTYWriter(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	file, err := os.Create(filepath.Join(tmp, "stdout.txt"))
	require.NoError(t, err)
	defer file.Close()

	app := &App{
		Stdout: file,
		Stderr: file,
	}

	ctx := context.Background()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "init", "--db", dbPath}))
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "--no-color", "status"}))
	require.NoError(t, file.Close())

	data, err := os.ReadFile(filepath.Join(tmp, "stdout.txt"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "\x1b[")
}

func TestCompletionBashOutput(t *testing.T) {
	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	require.NoError(t, app.Run(context.Background(), []string{"completion", "bash"}))

	out := stdout.String()
	require.Contains(t, out, "complete -F _slacrawl slacrawl")
	require.Contains(t, out, "completion")
	require.Contains(t, out, "report")
	require.Contains(t, out, "--format")
	require.Contains(t, out, "--kind")
}

func TestCompletionZshOutput(t *testing.T) {
	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	require.NoError(t, app.Run(context.Background(), []string{"completion", "zsh"}))

	out := stdout.String()
	require.Contains(t, out, "#compdef slacrawl")
	require.Contains(t, out, "_values 'shell' bash zsh")
	require.Contains(t, out, "report")
	require.Contains(t, out, "--no-color")
	require.Contains(t, out, "public_channel")
}

func TestReportIncludesArchiveAndShareState(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	dbPath := filepath.Join(tmp, "slacrawl.db")

	cfg := config.Default()
	cfg.DBPath = dbPath
	cfg.Share.Remote = "https://example.com/private/archive.git"
	cfg.Share.RepoPath = filepath.Join(tmp, "share")
	cfg.Share.AutoUpdate = false
	cfg.Slack.Bot.Enabled = false
	cfg.Slack.App.Enabled = false
	cfg.Slack.User.Enabled = false
	cfg.Slack.Desktop.Enabled = false
	require.NoError(t, cfg.Save(configPath))

	seedArchiveStore(t, dbPath, "archive report seed")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, st.SetSyncState(context.Background(), "share", "import", "last_import_at", mustTime(t, "2026-03-08T19:20:43Z").Format(time.RFC3339Nano)))
	require.NoError(t, st.SetSyncState(context.Background(), "share", "import", "last_manifest_generated_at", mustTime(t, "2026-03-08T19:10:43Z").Format(time.RFC3339Nano)))
	require.NoError(t, st.Close())

	var stdout bytes.Buffer
	app := &App{Stdout: &stdout, Stderr: &stdout}
	require.NoError(t, app.Run(context.Background(), []string{"--config", configPath, "--json", "report"}))

	var body map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &body))
	activity := body["activity"].(map[string]any)
	require.Equal(t, float64(1), activity["total_workspaces"])
	require.Equal(t, float64(1), activity["total_messages"])
	shareState := body["share"].(map[string]any)
	require.Equal(t, true, shareState["enabled"])
	require.Equal(t, "https://example.com/private/archive.git", shareState["remote"])
}

func TestPublishSubscribeAndSearchGitArchive(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	remoteRepo := filepath.Join(dir, "remote.git")
	runGit(t, dir, "init", "--bare", remoteRepo)

	publisherCfgPath := filepath.Join(dir, "publisher.toml")
	publisherDB := filepath.Join(dir, "publisher.db")
	publisherCfg := config.Default()
	publisherCfg.DBPath = publisherDB
	publisherCfg.Share.RepoPath = filepath.Join(dir, "publisher-share")
	publisherCfg.Share.Remote = remoteRepo
	require.NoError(t, publisherCfg.Save(publisherCfgPath))
	seedArchiveStore(t, publisherDB, "archive seed message")

	var stdout bytes.Buffer
	app := &App{Stdout: &stdout, Stderr: &stdout}
	require.NoError(t, app.Run(ctx, []string{"--config", publisherCfgPath, "--json", "publish", "--push"}))

	readerCfgPath := filepath.Join(dir, "reader.toml")
	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", readerCfgPath, "--json", "subscribe", "--repo", filepath.Join(dir, "reader-share"), "--db", filepath.Join(dir, "reader.db"), remoteRepo}))
	var subscribe map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &subscribe))
	require.Equal(t, true, subscribe["imported"])

	cfg, err := config.Load(readerCfgPath)
	require.NoError(t, err)
	require.False(t, cfg.Slack.Bot.Enabled)
	require.False(t, cfg.Slack.App.Enabled)
	require.False(t, cfg.Slack.User.Enabled)
	require.False(t, cfg.Slack.Desktop.Enabled)

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", readerCfgPath, "--json", "search", "archive"}))
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.Equal(t, "archive seed message", rows[0]["text"])
}

func TestSearchAutoUpdatesStaleGitArchive(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	remoteRepo := filepath.Join(dir, "remote.git")
	runGit(t, dir, "init", "--bare", remoteRepo)

	publisherCfgPath := filepath.Join(dir, "publisher.toml")
	publisherDB := filepath.Join(dir, "publisher.db")
	publisherCfg := config.Default()
	publisherCfg.DBPath = publisherDB
	publisherCfg.Share.RepoPath = filepath.Join(dir, "publisher-share")
	publisherCfg.Share.Remote = remoteRepo
	require.NoError(t, publisherCfg.Save(publisherCfgPath))
	seedArchiveStore(t, publisherDB, "archive baseline")

	var stdout bytes.Buffer
	app := &App{Stdout: &stdout, Stderr: &stdout}
	require.NoError(t, app.Run(ctx, []string{"--config", publisherCfgPath, "--json", "publish", "--push"}))

	readerCfgPath := filepath.Join(dir, "reader.toml")
	readerCfg := config.Default()
	readerCfg.DBPath = filepath.Join(dir, "reader.db")
	readerCfg.Share.Remote = remoteRepo
	readerCfg.Share.RepoPath = filepath.Join(dir, "reader-share")
	readerCfg.Share.StaleAfter = "1h"
	readerCfg.Slack.Bot.Enabled = false
	readerCfg.Slack.App.Enabled = false
	readerCfg.Slack.User.Enabled = false
	readerCfg.Slack.Desktop.Enabled = false
	readerCfg.Slack.Desktop.Path = ""
	require.NoError(t, readerCfg.Save(readerCfgPath))

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", readerCfgPath, "--json", "update"}))

	appendArchiveMessage(t, publisherDB, "archive delta landed")
	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", publisherCfgPath, "--json", "publish", "--push"}))

	readerStore, err := store.Open(readerCfg.DBPath)
	require.NoError(t, err)
	require.NoError(t, readerStore.SetSyncState(ctx, "share", "import", "last_import_at", time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339Nano)))
	require.NoError(t, readerStore.Close())

	stdout.Reset()
	require.NoError(t, app.Run(ctx, []string{"--config", readerCfgPath, "--json", "search", "delta"}))
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.Equal(t, "archive delta landed", rows[0]["text"])
}

func seedArchiveStore(t *testing.T, dbPath string, message string) {
	t.Helper()
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	now := mustTime(t, "2026-03-08T18:20:43Z")
	require.NoError(t, st.UpsertWorkspace(context.Background(), store.Workspace{ID: "T1", Name: "one", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(context.Background(), store.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertUser(context.Background(), store.User{ID: "U1", WorkspaceID: "T1", Name: "alice", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ChannelID:      "C1",
		TS:             "1710000000.000100",
		WorkspaceID:    "T1",
		UserID:         "U1",
		Text:           message,
		NormalizedText: message,
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, []store.Mention{{Type: "user", TargetID: "U1", DisplayText: "alice"}}))
}

func appendArchiveMessage(t *testing.T, dbPath string, message string) {
	t.Helper()
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	now := mustTime(t, "2026-03-08T19:20:43Z")
	require.NoError(t, st.UpsertMessage(context.Background(), store.Message{
		ChannelID:      "C1",
		TS:             "1710003600.000200",
		WorkspaceID:    "T1",
		UserID:         "U1",
		Text:           message,
		NormalizedText: message,
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, []store.Mention{{Type: "user", TargetID: "U1", DisplayText: "alice"}}))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}
