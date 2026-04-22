package report

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vincentkoc/slacrawl/internal/store"
)

func TestBuildReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "report.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	require.NoError(t, st.UpsertWorkspace(ctx, store.Workspace{ID: "T1", Name: "team", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C1", WorkspaceID: "T1", Name: "eng", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertChannel(ctx, store.Channel{ID: "C2", WorkspaceID: "T1", Name: "ops", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertUser(ctx, store.User{ID: "U1", WorkspaceID: "T1", Name: "alice", DisplayName: "Alice", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertUser(ctx, store.User{ID: "U2", WorkspaceID: "T1", Name: "bob", RealName: "Bob", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID:      "C1",
		TS:             "1776852000.000100",
		WorkspaceID:    "T1",
		UserID:         "U1",
		Text:           "hello",
		NormalizedText: "hello",
		EditedTS:       "1776852600.000100",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, nil))
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID:      "C2",
		TS:             "1776938400.000200",
		WorkspaceID:    "T1",
		UserID:         "U2",
		Text:           "incident",
		NormalizedText: "incident",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, nil))
	require.NoError(t, st.UpsertMessage(ctx, store.Message{
		ChannelID:      "C2",
		TS:             "draft:1776938400.000200:C2",
		WorkspaceID:    "T1",
		UserID:         "U2",
		Text:           "draft note",
		NormalizedText: "draft note",
		SourceRank:     2,
		SourceName:     "desktop",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, nil))

	report, err := Build(ctx, st, Options{Now: now})
	require.NoError(t, err)
	require.Equal(t, 1, report.TotalWorkspaces)
	require.Equal(t, 2, report.TotalChannels)
	require.Equal(t, 2, report.TotalUsers)
	require.Equal(t, 3, report.TotalMessages)
	require.Equal(t, 1, report.DraftMessages)
	require.Equal(t, 1, report.EditedMessages)
	require.Equal(t, time.Unix(1776938400, 0).UTC(), report.LatestMessageAt)
	require.Len(t, report.Windows, 3)
	require.NotEmpty(t, report.TopChannels)
	require.NotEmpty(t, report.TopAuthors)
	require.NotEmpty(t, report.BusiestDays)
}
