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

func TestResolveTokensForWorkspace(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-default")
	t.Setenv("SLACK_APP_TOKEN", "xapp-default")
	t.Setenv("SLACK_USER_TOKEN", "xoxp-default")
	t.Setenv("SLACK_ALPHA_BOT_TOKEN", "xoxb-alpha")
	t.Setenv("SLACK_ALPHA_APP_TOKEN", "xapp-alpha")
	t.Setenv("SLACK_ALPHA_USER_TOKEN", "xoxp-alpha")

	cfg := Default()
	cfg.Workspaces = []Workspace{{
		ID:           "TALPHA",
		BotTokenEnv:  "SLACK_ALPHA_BOT_TOKEN",
		AppTokenEnv:  "SLACK_ALPHA_APP_TOKEN",
		UserTokenEnv: "SLACK_ALPHA_USER_TOKEN",
	}}

	tokens := cfg.ResolveTokensForWorkspace("TALPHA")
	require.Equal(t, "xoxb-alpha", tokens.Bot)
	require.Equal(t, "xapp-alpha", tokens.App)
	require.Equal(t, "xoxp-alpha", tokens.User)
	require.Equal(t, "xoxb-default", cfg.ResolveTokensForWorkspace("TUNKNOWN").Bot)
}

func TestResolveTokensForWorkspaceUsesImplicitWorkspaceEnvNames(t *testing.T) {
	t.Setenv("SLACK_TALPHA_BOT_TOKEN", "xoxb-alpha")
	t.Setenv("SLACK_TALPHA_APP_TOKEN", "xapp-alpha")
	t.Setenv("SLACK_TALPHA_USER_TOKEN", "xoxp-alpha")

	cfg := Default()
	cfg.Workspaces = []Workspace{{ID: "TALPHA"}}

	tokens := cfg.ResolveTokensForWorkspace("TALPHA")
	require.Equal(t, "xoxb-alpha", tokens.Bot)
	require.Equal(t, "xapp-alpha", tokens.App)
	require.Equal(t, "xoxp-alpha", tokens.User)
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

func TestNormalizeSetsDefaultWorkspaceFromWorkspaceList(t *testing.T) {
	cfg := Default()
	cfg.Workspaces = []Workspace{
		{ID: "T123"},
		{ID: "T456", Default: true},
	}
	require.NoError(t, cfg.Normalize())
	require.Equal(t, "T456", cfg.WorkspaceID)
	require.Equal(t, []string{"T123", "T456"}, cfg.WorkspaceIDs())
}

func mustHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	return home
}
