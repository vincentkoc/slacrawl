package share

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vincentkoc/slacrawl/internal/store"
)

func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	source := seedStore(t, filepath.Join(dir, "source.db"))
	defer source.Close()

	opts := Options{RepoPath: filepath.Join(dir, "share"), Branch: "main"}
	manifest, err := Export(ctx, source, opts)
	require.NoError(t, err)
	require.Equal(t, 1, manifest.Version)
	require.NotEmpty(t, manifest.Tables)
	require.FileExists(t, filepath.Join(opts.RepoPath, ManifestName))

	reader, err := store.Open(filepath.Join(dir, "reader.db"))
	require.NoError(t, err)
	defer reader.Close()

	imported, err := Import(ctx, reader, opts)
	require.NoError(t, err)
	require.Equal(t, manifest.GeneratedAt.UTC().Format(time.RFC3339Nano), imported.GeneratedAt.UTC().Format(time.RFC3339Nano))

	rows, err := reader.Search(ctx, "", "archive", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "git backed archive works", rows[0].Text)
}

func TestImportIfChangedSkipsCurrentManifest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	source := seedStore(t, filepath.Join(dir, "source.db"))
	defer source.Close()

	opts := Options{RepoPath: filepath.Join(dir, "share"), Branch: "main"}
	manifest, err := Export(ctx, source, opts)
	require.NoError(t, err)

	reader, err := store.Open(filepath.Join(dir, "reader.db"))
	require.NoError(t, err)
	defer reader.Close()

	imported, changed, err := ImportIfChanged(ctx, reader, opts)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, manifest.GeneratedAt.UTC().Format(time.RFC3339Nano), imported.GeneratedAt.UTC().Format(time.RFC3339Nano))

	_, changed, err = ImportIfChanged(ctx, reader, opts)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestNeedsImportUsesLastImportTime(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := seedStore(t, filepath.Join(dir, "source.db"))
	defer s.Close()

	require.True(t, NeedsImport(ctx, s, time.Hour))

	require.NoError(t, s.SetSyncState(ctx, importSyncSource, importSyncEntityType, lastImportEntityID, time.Now().UTC().Format(time.RFC3339Nano)))
	require.False(t, NeedsImport(ctx, s, time.Hour))
	require.NoError(t, s.SetSyncState(ctx, importSyncSource, importSyncEntityType, lastImportEntityID, time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339Nano)))
	require.True(t, NeedsImport(ctx, s, time.Hour))
}

func seedStore(t *testing.T, path string) *store.Store {
	t.Helper()
	s, err := store.Open(path)
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.UpsertWorkspace(ctx, store.Workspace{
		ID:        "T1",
		Name:      "team",
		RawJSON:   "{}",
		UpdatedAt: now,
	}))
	require.NoError(t, s.UpsertChannel(ctx, store.Channel{
		ID:          "C1",
		WorkspaceID: "T1",
		Name:        "general",
		Kind:        "public_channel",
		RawJSON:     "{}",
		UpdatedAt:   now,
	}))
	require.NoError(t, s.UpsertUser(ctx, store.User{
		ID:          "U1",
		WorkspaceID: "T1",
		Name:        "alice",
		RawJSON:     "{}",
		UpdatedAt:   now,
	}))
	require.NoError(t, s.UpsertMessage(ctx, store.Message{
		ChannelID:      "C1",
		TS:             "123.456",
		WorkspaceID:    "T1",
		UserID:         "U1",
		Text:           "git backed archive works",
		NormalizedText: "git backed archive works",
		SourceRank:     2,
		SourceName:     "api-bot",
		RawJSON:        "{}",
		UpdatedAt:      now,
	}, []store.Mention{{Type: "user", TargetID: "U1", DisplayText: "alice"}}))
	require.NoError(t, s.SetSyncState(ctx, "api-bot", "workspace", "T1", now.Format(time.RFC3339Nano)))
	return s
}
