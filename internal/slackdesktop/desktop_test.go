package slackdesktop

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRootState(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "desktop", "root-state.json")
	summary, err := LoadRootState(path)
	require.NoError(t, err)
	require.Equal(t, 2, summary.WorkspaceCount)
	require.Equal(t, 1, summary.TeamsCount)
	require.Equal(t, 2, len(summary.AppTeamsKeys))
}
