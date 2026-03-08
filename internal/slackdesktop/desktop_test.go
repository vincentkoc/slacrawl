package slackdesktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/vincentkoc/slacrawl/internal/store"
)

func TestLoadRootState(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "desktop", "root-state.json")
	root, err := LoadRootState(path)
	require.NoError(t, err)
	require.Equal(t, 2, root.Summary.WorkspaceCount)
	require.Equal(t, 1, root.Summary.TeamsCount)
	require.Equal(t, 2, len(root.Summary.AppTeamsKeys))
	require.Equal(t, 1, root.Summary.DownloadTeamCount)
	require.Equal(t, 1, root.Summary.DownloadItemCount)
}

func TestParseLocalStorage(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "leveldb")
	db, err := leveldb.OpenFile(dbPath, nil)
	require.NoError(t, err)
	require.NoError(t, db.Put([]byte("_https://app.slack.comlocalConfig_v2"), []byte(`{"teams":{"T111":{"id":"T111","name":"Team One","domain":"team-one","user_id":"U111","token":"xoxc-secret"}}}`), nil))
	require.NoError(t, db.Put([]byte("_https://app.slack.compersist-v1::T111::U111::drafts"), []byte(`{"unifiedDrafts":{"draft-1":{"id":"draft-1","client_draft_id":"draft-1","destinations":[{"channel_id":"C111","thread_ts":"1710000000.000100"}],"ops":[{"insert":"hello "},{"insert":"world"}],"last_updated_ts":1710000001.000200}}}`), nil))
	require.NoError(t, db.Put([]byte("_https://app.slack.comactivitySession_T111"), []byte(`{"session-1":{"id":"session-1","startTime":1,"lastActivity":2,"lastLogged":3}}`), nil))
	require.NoError(t, db.Close())

	data, err := ParseLocalStorage(dbPath)
	require.NoError(t, err)
	require.Equal(t, 1, data.Summary.WorkspaceCount)
	require.Equal(t, 1, data.Summary.DraftCount)
	require.Equal(t, 1, data.Summary.ActivityTeamCount)
	require.Equal(t, "Team One", data.LocalConfig.Teams["T111"].Name)
	require.Equal(t, "hello world", draftText(data.Drafts[0]))
}

func TestIngestDesktopState(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "storage"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Local Storage", "leveldb"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "IndexedDB", "https_app.slack.com_0.indexeddb.leveldb"), 0o755))

	rootStatePath := filepath.Join(root, "storage", "root-state.json")
	rootStateData, err := os.ReadFile(filepath.Join("..", "..", "testdata", "desktop", "root-state.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(rootStatePath, rootStateData, 0o644))

	localDB, err := leveldb.OpenFile(filepath.Join(root, "Local Storage", "leveldb"), nil)
	require.NoError(t, err)
	require.NoError(t, localDB.Put([]byte("_https://app.slack.comlocalConfig_v2"), []byte(`{"teams":{"T111":{"id":"T111","name":"Team One","domain":"team-one","user_id":"U111","token":"xoxc-secret"}}}`), nil))
	require.NoError(t, localDB.Put([]byte("_https://app.slack.compersist-v1::T111::U111::drafts"), []byte(`{"unifiedDrafts":{"draft-1":{"id":"draft-1","client_draft_id":"draft-1","destinations":[{"channel_id":"C111"}],"ops":[{"insert":"draft body"}],"last_updated_ts":1710000001.000200}}}`), nil))
	require.NoError(t, localDB.Close())

	indexDB, err := leveldb.OpenFile(filepath.Join(root, "IndexedDB", "https_app.slack.com_0.indexeddb.leveldb"), &opt.Options{Comparer: indexedDBComparer{}})
	require.NoError(t, err)
	require.NoError(t, indexDB.Put([]byte("https_app.slack.com_0@1#objectStore-T111-U111"), []byte("A"), nil))
	require.NoError(t, indexDB.Close())

	st, err := store.Open(filepath.Join(t.TempDir(), "slacrawl.db"))
	require.NoError(t, err)
	defer st.Close()

	source, err := Ingest(context.Background(), st, root)
	require.NoError(t, err)
	require.True(t, source.Available)
	require.Equal(t, 1, source.Local.DraftCount)
	require.Len(t, source.IndexedDB.ObjectStores, 1)

	status, err := st.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, status.Workspaces)
	require.Equal(t, 1, status.Messages)
}
