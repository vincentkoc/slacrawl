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

func TestBuildDigest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "digest.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-48 * time.Hour)
	stale := now.Add(-20 * 24 * time.Hour)

	require.NoError(t, st.UpsertWorkspace(ctx, store.Workspace{ID: "T1", Name: "team", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C1", WorkspaceID: "T1", Name: "engineering", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C2", WorkspaceID: "T1", Name: "ops", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C3", WorkspaceID: "T1", Name: "old-stuff", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))

	require.NoError(t, st.UpsertUser(ctx, store.User{ID: "U1", WorkspaceID: "T1", Name: "alice", DisplayName: "Alice", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertUser(ctx, store.User{ID: "U2", WorkspaceID: "T1", Name: "bob", RealName: "Bob", RawJSON: "{}", UpdatedAt: now}))

	// C1: 3 messages from Alice within the 7d window, one thread parent
	addMsg(t, ctx, st, "C1", tsEpoch(earlier, 100), "T1", "U1", "", "hello team")
	addMsg(t, ctx, st, "C1", tsEpoch(earlier, 200), "T1", "U1", tsEpoch(earlier, 200), "thread parent")
	addMsg(t, ctx, st, "C1", tsEpoch(earlier.Add(time.Hour), 300), "T1", "U2", tsEpoch(earlier, 200), "replying")

	// Mention of U2 inside C1 during window
	addMsgWithMentions(t, ctx, st, "C1", tsEpoch(earlier.Add(2*time.Hour), 400), "T1", "U1", "", "yo <@U2>", []store.Mention{
		{Type: "user", TargetID: "U2", DisplayText: "Bob"},
	})

	// C2: 1 message from Bob in window
	addMsg(t, ctx, st, "C2", tsEpoch(earlier.Add(3*time.Hour), 500), "T1", "U2", "", "ops only")

	// C3: old message (outside 7d window) - should not appear in digest
	addMsg(t, ctx, st, "C3", tsEpoch(stale, 600), "T1", "U1", "", "old news")

	// Draft message - should be ignored
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID:      "C2",
		TS:             "draft:" + tsEpoch(earlier, 999) + ":C2",
		WorkspaceID:    "T1",
		UserID:         "U2",
		Text:           "not-yet-sent",
		NormalizedText: "not-yet-sent",
		SourceRank:     2,
		SourceName:     "desktop",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, nil))

	t.Run("default window lists recent channels only", func(t *testing.T) {
		d, err := BuildDigest(ctx, st, DigestOptions{Now: now})
		require.NoError(t, err)
		require.Equal(t, "7d", d.WindowLabel)
		require.Equal(t, 1, d.TopN)
		// C3 has only a message 20d ago; it must not appear in the 7d digest
		require.Len(t, d.Channels, 2)
		require.Equal(t, "C1", d.Channels[0].ChannelID)
		require.Equal(t, "engineering", d.Channels[0].ChannelName)
		require.Equal(t, 4, d.Channels[0].Messages)
		require.Equal(t, 1, d.Channels[0].Threads)
		require.Equal(t, 2, d.Channels[0].ActiveAuthors)
		require.Equal(t, "C2", d.Channels[1].ChannelID)
		require.Equal(t, 1, d.Channels[1].Messages)
		require.Equal(t, 0, d.Channels[1].Threads)
		// Top poster of C1 is Alice with 3 messages
		require.Len(t, d.Channels[0].TopPosters, 1)
		require.Equal(t, "Alice", d.Channels[0].TopPosters[0].Name)
		require.Equal(t, 3, d.Channels[0].TopPosters[0].Count)
		// Top mention of C1 is Bob (single mention)
		require.Len(t, d.Channels[0].TopMentions, 1)
		require.Equal(t, "Bob", d.Channels[0].TopMentions[0].Name)
		// Totals
		require.Equal(t, 5, d.Totals.Messages)
		require.Equal(t, 1, d.Totals.Threads)
		require.Equal(t, 2, d.Totals.Channels)
	})

	t.Run("channel filter drills into one channel", func(t *testing.T) {
		d, err := BuildDigest(ctx, st, DigestOptions{Now: now, Channel: "engineering", TopN: 5})
		require.NoError(t, err)
		require.Len(t, d.Channels, 1)
		require.Equal(t, "C1", d.Channels[0].ChannelID)
		// With TopN=5 we still see only the two actual posters
		require.Len(t, d.Channels[0].TopPosters, 2)
	})

	t.Run("extended window picks up old channel", func(t *testing.T) {
		d, err := BuildDigest(ctx, st, DigestOptions{Now: now, Since: 30 * 24 * time.Hour})
		require.NoError(t, err)
		require.Equal(t, "30d", d.WindowLabel)
		require.Len(t, d.Channels, 3)
	})

	t.Run("workspace filter respects scope", func(t *testing.T) {
		d, err := BuildDigest(ctx, st, DigestOptions{Now: now, WorkspaceID: "T-other"})
		require.NoError(t, err)
		require.Empty(t, d.Channels)
		require.Equal(t, 0, d.Totals.Messages)
	})

	t.Run("hour-granular window renders label", func(t *testing.T) {
		d, err := BuildDigest(ctx, st, DigestOptions{Now: now, Since: 72 * time.Hour})
		require.NoError(t, err)
		require.Equal(t, "3d", d.WindowLabel)
	})

	t.Run("negative since is normalized", func(t *testing.T) {
		d, err := BuildDigest(ctx, st, DigestOptions{Now: now, Since: -48 * time.Hour})
		require.NoError(t, err)
		require.Equal(t, "2d", d.WindowLabel)
	})
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0"},
		{24 * time.Hour, "1d"},
		{7 * 24 * time.Hour, "7d"},
		{36 * time.Hour, "36h"},
		{90 * time.Minute, "1h30m0s"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, humanDuration(c.in), "duration=%s", c.in)
	}
}

// tsEpoch renders a Slack-style message TS in the form "SECONDS.MICROSECONDS".
func tsEpoch(at time.Time, micros int) string {
	return fmt.Sprintf("%d.%06d", at.UTC().Unix(), micros)
}

func addMsg(t *testing.T, ctx context.Context, st *store.Store, channelID, ts, workspaceID, userID, threadTS, text string) {
	t.Helper()
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID:      channelID,
		TS:             ts,
		WorkspaceID:    workspaceID,
		UserID:         userID,
		ThreadTS:       threadTS,
		Text:           text,
		NormalizedText: text,
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      time.Unix(0, 0).UTC(),
	}, nil))
}

func addMsgWithMentions(t *testing.T, ctx context.Context, st *store.Store, channelID, ts, workspaceID, userID, threadTS, text string, mentions []store.Mention) {
	t.Helper()
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID:      channelID,
		TS:             ts,
		WorkspaceID:    workspaceID,
		UserID:         userID,
		ThreadTS:       threadTS,
		Text:           text,
		NormalizedText: text,
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      time.Unix(0, 0).UTC(),
	}, mentions))
}
