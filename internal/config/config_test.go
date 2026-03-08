package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandPath(t *testing.T) {
	expanded, err := ExpandPath("~/tmp/slacrawl")
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(expanded))
}

func TestResolveTokens(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("SLACK_USER_TOKEN", "xoxp-test")

	cfg := Default()
	tokens := cfg.ResolveTokens()
	require.Equal(t, "xoxb-test", tokens.Bot)
	require.Equal(t, "xapp-test", tokens.App)
	require.Equal(t, "xoxp-test", tokens.User)
}

func TestResolveTokensHonorsEnabledFlags(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("SLACK_USER_TOKEN", "xoxp-test")

	cfg := Default()
	cfg.Slack.App.Enabled = false
	cfg.Slack.User.Enabled = false

	tokens := cfg.ResolveTokens()
	require.Equal(t, "xoxb-test", tokens.Bot)
	require.Equal(t, "", tokens.App)
	require.Equal(t, "", tokens.User)
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.WorkspaceID = "T123"
	require.NoError(t, cfg.Save(path))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "T123", loaded.WorkspaceID)
	require.True(t, filepath.IsAbs(loaded.DBPath))
}

func TestDefaultConfigPath(t *testing.T) {
	path, err := DefaultConfigPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(mustHome(t), ".slacrawl", "config.toml"), path)
}

func TestNormalizeAutoDetectsDesktopPath(t *testing.T) {
	cfg := Default()
	cfg.Slack.Desktop.Path = ""
	require.NoError(t, cfg.Normalize())
	require.True(t, filepath.IsAbs(cfg.Slack.Desktop.Path))
}

func mustHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	return home
}
