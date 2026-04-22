package report

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vincentkoc/slacrawl/internal/store"
)

func TestBuildTrends(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "trends.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()
	now := time.Unix(1776852000, 0).UTC() // 2026-04-22T12:00:00Z
	nowBucket := now.Unix() / secondsPerWeek

	require.NoError(t, st.UpsertWorkspace(ctx, store.Workspace{ID: "T1", Name: "team", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C1", WorkspaceID: "T1", Name: "alpha", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C2", WorkspaceID: "T1", Name: "beta", Kind: "private_channel", RawJSON: "{}", UpdatedAt: now}))

	seed := func(channelID string, bucket int64, count int) {
		for i := 0; i < count; i++ {
			ts := fmt.Sprintf("%d.%06d", bucket*secondsPerWeek+int64(10+i), i+1)
			addMsg(t, ctx, st, channelID, ts, "T1", "U1", "", "message")
		}
	}

	seed("C1", nowBucket-2, 2)
	seed("C1", nowBucket-1, 3)
	seed("C1", nowBucket, 1)

	seed("C2", nowBucket-1, 2)
	seed("C2", nowBucket, 4)

	trends, err := BuildTrends(ctx, st, TrendsOptions{Now: now, Weeks: 3})
	require.NoError(t, err)
	require.Equal(t, 3, trends.Weeks)
	require.Len(t, trends.Rows, 2)

	require.Equal(t, "alpha", trends.Rows[0].ChannelName)
	require.Equal(t, "beta", trends.Rows[1].ChannelName)

	alpha := trends.Rows[0].Weekly
	require.Len(t, alpha, 3)
	require.Equal(t, 2, alpha[0].Messages)
	require.Equal(t, 3, alpha[1].Messages)
	require.Equal(t, 1, alpha[2].Messages)

	beta := trends.Rows[1].Weekly
	require.Len(t, beta, 3)
	require.Equal(t, 0, beta[0].Messages)
	require.Equal(t, 2, beta[1].Messages)
	require.Equal(t, 4, beta[2].Messages)

	for i, bucket := range []int64{nowBucket - 2, nowBucket - 1, nowBucket} {
		require.Equal(t, time.Unix(bucket*secondsPerWeek, 0).UTC(), alpha[i].WeekStart)
		require.Equal(t, time.Unix(bucket*secondsPerWeek, 0).UTC(), beta[i].WeekStart)
	}
}
