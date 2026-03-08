package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vincentkoc/slacrawl/internal/config"
	"github.com/vincentkoc/slacrawl/internal/store"
)

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
	require.True(t, strings.Contains(out, "never") || strings.Contains(out, "last sync"))
	require.NotContains(t, out, "\x1b[")
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
	require.Contains(t, out, "--format")
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
	require.Contains(t, out, "--no-color")
}
