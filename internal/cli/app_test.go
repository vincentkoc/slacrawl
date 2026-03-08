package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitStatusAndSQL(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: &stdout,
	}

	ctx := context.Background()
	require.NoError(t, app.Run(ctx, []string{"--config", configPath, "init"}))

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
