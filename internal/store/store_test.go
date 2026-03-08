package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	require.NoError(t, s.UpsertWorkspace(ctx, Workspace{
		ID:        "T1",
		Name:      "team",
		RawJSON:   "{}",
		UpdatedAt: time.Now().UTC(),
	}))
	require.NoError(t, s.UpsertChannel(ctx, Channel{
		ID:          "C1",
		WorkspaceID: "T1",
		Name:        "eng",
		Kind:        "public_channel",
		RawJSON:     "{}",
		UpdatedAt:   time.Now().UTC(),
	}))
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ChannelID:      "C1",
		TS:             "123.45",
		WorkspaceID:    "T1",
		Text:           "hello world",
		NormalizedText: "hello world",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      time.Now().UTC(),
	}, nil))

	results, err := s.Search(ctx, "", "hello", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "T1", results[0].WorkspaceID)
	status, err := s.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, status.Messages)
}

func TestUpsertMessageDeduplicatesMentions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ChannelID:      "C1",
		TS:             "123.45",
		WorkspaceID:    "T1",
		Text:           "<@U1> hello <@U1>",
		NormalizedText: "@u1 hello @u1",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      time.Now().UTC(),
	}, []Mention{
		{Type: "user", TargetID: "U1", DisplayText: "alice"},
		{Type: "user", TargetID: "U1", DisplayText: "alice"},
	}))

	rows, err := s.QueryReadOnly(ctx, "select count(*) as n from message_mentions where channel_id = 'C1' and ts = '123.45'")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(1), rows[0]["n"])
}

func TestWorkspaceFiltersApplyToReadQueries(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, s.UpsertWorkspace(ctx, Workspace{ID: "T1", Name: "one", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, s.UpsertWorkspace(ctx, Workspace{ID: "T2", Name: "two", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, s.UpsertChannel(ctx, Channel{ID: "C1", WorkspaceID: "T1", Name: "eng", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, s.UpsertChannel(ctx, Channel{ID: "C2", WorkspaceID: "T2", Name: "ops", Kind: "public_channel", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, s.UpsertUser(ctx, User{ID: "U1", WorkspaceID: "T1", Name: "alice", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, s.UpsertUser(ctx, User{ID: "U2", WorkspaceID: "T2", Name: "bob", RawJSON: "{}", UpdatedAt: now}))
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ChannelID:      "C1",
		TS:             "1.0",
		WorkspaceID:    "T1",
		UserID:         "U1",
		Text:           "hello alpha",
		NormalizedText: "hello alpha",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, []Mention{{Type: "user", TargetID: "U1", DisplayText: "alice"}}))
	require.NoError(t, s.UpsertMessage(ctx, Message{
		ChannelID:      "C2",
		TS:             "2.0",
		WorkspaceID:    "T2",
		UserID:         "U2",
		Text:           "hello beta",
		NormalizedText: "hello beta",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, []Mention{{Type: "user", TargetID: "U2", DisplayText: "bob"}}))

	messages, err := s.Messages(ctx, "T1", "", "", 10)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "T1", messages[0].WorkspaceID)

	search, err := s.Search(ctx, "T2", "hello", 10)
	require.NoError(t, err)
	require.Len(t, search, 1)
	require.Equal(t, "T2", search[0].WorkspaceID)

	mentions, err := s.Mentions(ctx, "T1", "U1", 10)
	require.NoError(t, err)
	require.Len(t, mentions, 1)
	require.Equal(t, "T1", mentions[0].WorkspaceID)

	users, err := s.Users(ctx, "T2", "", 10)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, "T2", users[0].WorkspaceID)

	channels, err := s.Channels(ctx, "T1", "", 10)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, "T1", channels[0].WorkspaceID)
}
