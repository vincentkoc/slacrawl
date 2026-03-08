package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

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
