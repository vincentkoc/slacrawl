package report

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vincentkoc/slacrawl/internal/store"
)

func TestBuildQuiet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "quiet.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * 24 * time.Hour)
	stale := now.Add(-45 * 24 * time.Hour)

	require.NoError(t, st.UpsertWorkspace(ctx, store.Workspace{ID: "T1", Name: "team", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C-quiet", WorkspaceID: "T1", Name: "quiet", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C-recent", WorkspaceID: "T1", Name: "recent", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C-stale", WorkspaceID: "T1", Name: "stale", Kind: "private_channel", RawJSON: "{}", UpdatedAt: now}))

	addMsg(t, ctx, st, "C-recent", tsEpoch(recent, 100), "T1", "U1", "", "new activity")
	addMsg(t, ctx, st, "C-stale", tsEpoch(stale, 200), "T1", "U1", "", "old activity")

	quiet, err := BuildQuiet(ctx, st, QuietOptions{Now: now})
	require.NoError(t, err)
	require.Equal(t, now.Add(-30*24*time.Hour), quiet.Since)
	require.Equal(t, now, quiet.Until)
	require.Len(t, quiet.Channels, 2)
	require.Equal(t, 2, quiet.Totals.Channels)

	require.Equal(t, "C-quiet", quiet.Channels[0].ChannelID)
	require.Empty(t, quiet.Channels[0].LastMessage)
	require.Equal(t, 30, quiet.Channels[0].DaysSilent)

	require.Equal(t, "C-stale", quiet.Channels[1].ChannelID)
	require.Equal(t, stale.Format(time.RFC3339), quiet.Channels[1].LastMessage)
	require.GreaterOrEqual(t, quiet.Channels[1].DaysSilent, 45)

	for _, row := range quiet.Channels {
		require.NotEqual(t, "C-recent", row.ChannelID)
	}
}
