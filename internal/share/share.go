package share

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vincentkoc/crawlkit/gitshare"
	"github.com/vincentkoc/crawlkit/pack"
	"github.com/vincentkoc/slacrawl/internal/store"
)

const (
	ManifestName = "manifest.json"

	importSyncSource           = "share"
	importSyncEntityType       = "import"
	lastImportEntityID         = "last_import_at"
	lastManifestEntityID       = "last_manifest_generated_at"
	defaultBranch              = "main"
	shardFlushRows             = 1024
	defaultMaxShardBytes int64 = 40 * 1024 * 1024
)

var ErrNoManifest = pack.ErrNoManifest

var maxShardBytes = defaultMaxShardBytes

var SnapshotTables = []string{
	"workspaces",
	"channels",
	"users",
	"messages",
	"message_events",
	"message_mentions",
	"sync_state",
	"embedding_jobs",
}

type Options struct {
	RepoPath string
	Remote   string
	Branch   string
}

type Manifest = pack.Manifest

type TableManifest = pack.TableManifest

type SyncState struct {
	LastImportAt            time.Time `json:"last_import_at"`
	LastManifestGeneratedAt time.Time `json:"last_manifest_generated_at"`
}

func EnsureRepo(ctx context.Context, opts Options) error {
	return gitshare.EnsureRepo(ctx, gitshareOptions(opts))
}

func Pull(ctx context.Context, opts Options) error {
	return gitshare.Pull(ctx, gitshareOptions(opts))
}

func Commit(ctx context.Context, opts Options, message string) (bool, error) {
	return gitshare.Commit(ctx, gitshareOptions(opts), message)
}

func Push(ctx context.Context, opts Options) error {
	return gitshare.Push(ctx, gitshareOptions(opts))
}

func Export(ctx context.Context, s *store.Store, opts Options) (Manifest, error) {
	if err := EnsureRepo(ctx, opts); err != nil {
		return Manifest{}, err
	}
	return pack.Export(ctx, pack.ExportOptions{DB: s.DB(), RootDir: opts.RepoPath, Tables: SnapshotTables})
}

func Import(ctx context.Context, s *store.Store, opts Options) (Manifest, error) {
	manifest, err := pack.Import(ctx, pack.ImportOptions{DB: s.DB(), RootDir: opts.RepoPath, DeleteTables: SnapshotTables})
	if err != nil {
		return Manifest{}, err
	}
	if err := s.RebuildSearchIndexes(ctx); err != nil {
		return Manifest{}, err
	}
	if err := MarkImported(ctx, s, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ImportIfChanged(ctx context.Context, s *store.Store, opts Options) (Manifest, bool, error) {
	manifest, err := pack.ReadManifest(opts.RepoPath)
	if err != nil {
		return Manifest{}, false, err
	}
	if ManifestAlreadyImported(ctx, s, manifest) {
		if err := MarkImported(ctx, s, manifest); err != nil {
			return Manifest{}, false, err
		}
		return manifest, false, nil
	}
	imported, err := Import(ctx, s, opts)
	if err != nil {
		return Manifest{}, false, err
	}
	return imported, true, nil
}

func ManifestAlreadyImported(ctx context.Context, s *store.Store, manifest Manifest) bool {
	if manifest.GeneratedAt.IsZero() {
		return false
	}
	last, err := s.GetSyncState(ctx, importSyncSource, importSyncEntityType, lastManifestEntityID)
	if err != nil || strings.TrimSpace(last) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, last)
	if err != nil {
		return false
	}
	return t.Equal(manifest.GeneratedAt)
}

func MarkImported(ctx context.Context, s *store.Store, manifest Manifest) error {
	if err := s.SetSyncState(ctx, importSyncSource, importSyncEntityType, lastImportEntityID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if manifest.GeneratedAt.IsZero() {
		return nil
	}
	return s.SetSyncState(ctx, importSyncSource, importSyncEntityType, lastManifestEntityID, manifest.GeneratedAt.Format(time.RFC3339Nano))
}

func NeedsImport(ctx context.Context, s *store.Store, staleAfter time.Duration) bool {
	if staleAfter <= 0 {
		staleAfter = 15 * time.Minute
	}
	last, err := s.GetSyncState(ctx, importSyncSource, importSyncEntityType, lastImportEntityID)
	if err != nil || strings.TrimSpace(last) == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339Nano, last)
	if err != nil {
		return true
	}
	return time.Since(t) >= staleAfter
}

func ReadSyncState(ctx context.Context, s *store.Store) (SyncState, error) {
	var state SyncState
	lastImport, err := s.GetSyncState(ctx, importSyncSource, importSyncEntityType, lastImportEntityID)
	if err == nil {
		state.LastImportAt = parseSyncTime(lastImport)
	}
	lastManifest, err := s.GetSyncState(ctx, importSyncSource, importSyncEntityType, lastManifestEntityID)
	if err == nil {
		state.LastManifestGeneratedAt = parseSyncTime(lastManifest)
	}
	return state, nil
}

func ReadManifest(repoPath string) (Manifest, error) {
	manifest, err := pack.ReadManifest(repoPath)
	if err != nil {
		return Manifest{}, err
	}
	if manifest.Version != 1 {
		return Manifest{}, fmt.Errorf("unsupported share manifest version %d", manifest.Version)
	}
	return manifest, nil
}

func gitshareOptions(opts Options) gitshare.Options {
	return gitshare.Options{RepoPath: opts.RepoPath, Remote: opts.Remote, Branch: normalizeBranch(opts.Branch)}
}

func exportTable(ctx context.Context, db *sql.DB, dataDir, table string) (TableManifest, error) {
	rows, err := db.QueryContext(ctx, "select * from "+quoteIdent(table))
	if err != nil {
		return TableManifest{}, fmt.Errorf("query %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return TableManifest{}, fmt.Errorf("columns %s: %w", table, err)
	}
	tableDir := filepath.Join(dataDir, table)
	if err := os.MkdirAll(tableDir, 0o755); err != nil {
		return TableManifest{}, fmt.Errorf("mkdir %s: %w", table, err)
	}
	writer := tableShardWriter{dataDir: dataDir, table: table}
	if err := writer.open(); err != nil {
		return TableManifest{}, err
	}
	defer func() { _ = writer.close() }()

	values := make([]any, len(columns))
	ptrs := make([]any, len(columns))
	for i := range values {
		ptrs[i] = &values[i]
	}
	count := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return TableManifest{}, fmt.Errorf("scan %s: %w", table, err)
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			row[column] = exportValue(values[i])
		}
		body, err := json.Marshal(row)
		if err != nil {
			return TableManifest{}, fmt.Errorf("marshal %s row: %w", table, err)
		}
		if err := writer.rotateIfNeeded(); err != nil {
			return TableManifest{}, err
		}
		if _, err := writer.Write(body); err != nil {
			return TableManifest{}, fmt.Errorf("write %s row: %w", table, err)
		}
		if _, err := writer.Write([]byte{'\n'}); err != nil {
			return TableManifest{}, fmt.Errorf("write %s newline: %w", table, err)
		}
		count++
		if err := writer.finishRow(); err != nil {
			return TableManifest{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return TableManifest{}, fmt.Errorf("iterate %s: %w", table, err)
	}
	if err := writer.close(); err != nil {
		return TableManifest{}, err
	}
	return TableManifest{Name: table, Files: writer.files, Columns: columns, Rows: count}, nil
}

func importTable(ctx context.Context, tx *sql.Tx, repoPath string, table TableManifest) error {
	files := table.Files
	if len(files) == 0 && strings.TrimSpace(table.File) != "" {
		files = []string{table.File}
	}
	if len(files) == 0 {
		return fmt.Errorf("manifest table %s has no files", table.Name)
	}
	stmt, err := tx.PrepareContext(ctx, insertSQL(table.Name, table.Columns))
	if err != nil {
		return fmt.Errorf("prepare import %s: %w", table.Name, err)
	}
	defer func() { _ = stmt.Close() }()
	for _, rel := range files {
		if err := importTableFile(ctx, stmt, repoPath, table, rel); err != nil {
			return err
		}
	}
	return nil
}

func importTableFile(ctx context.Context, stmt *sql.Stmt, repoPath string, table TableManifest, rel string) error {
	path := filepath.Join(repoPath, filepath.FromSlash(rel))
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", rel, err)
	}
	defer func() { _ = file.Close() }()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip %s: %w", rel, err)
	}
	defer func() { _ = gz.Close() }()
	dec := json.NewDecoder(gz)
	dec.UseNumber()
	for {
		row := map[string]any{}
		err := dec.Decode(&row)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("decode %s: %w", rel, err)
		}
		values := make([]any, len(table.Columns))
		for i, column := range table.Columns {
			values[i] = importValue(row[column])
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("insert %s: %w", table.Name, err)
		}
	}
	return nil
}

type tableShardWriter struct {
	dataDir     string
	table       string
	nextShard   int
	rowsInShard int
	files       []string
	file        *os.File
	counter     *countingWriter
	gz          *gzip.Writer
}

func (w *tableShardWriter) open() error {
	rel := filepath.ToSlash(filepath.Join("tables", w.table, fmt.Sprintf("%06d.jsonl.gz", w.nextShard)))
	path := filepath.Join(w.dataDir, w.table, fmt.Sprintf("%06d.jsonl.gz", w.nextShard))
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	w.nextShard++
	w.rowsInShard = 0
	w.files = append(w.files, rel)
	w.file = file
	w.counter = &countingWriter{w: file}
	w.gz = gzip.NewWriter(w.counter)
	return nil
}

func (w *tableShardWriter) Write(p []byte) (int, error) {
	return w.gz.Write(p)
}

func (w *tableShardWriter) rotateIfNeeded() error {
	if maxShardBytes <= 0 || w.rowsInShard == 0 || w.counter.n < maxShardBytes {
		return nil
	}
	if err := w.close(); err != nil {
		return err
	}
	return w.open()
}

func (w *tableShardWriter) finishRow() error {
	w.rowsInShard++
	if maxShardBytes > 1024*1024 && w.rowsInShard%shardFlushRows != 0 {
		return nil
	}
	if err := w.gz.Flush(); err != nil {
		return fmt.Errorf("flush %s shard: %w", w.table, err)
	}
	return nil
}

func (w *tableShardWriter) close() error {
	var closeErr error
	if w.gz != nil {
		if err := w.gz.Close(); err != nil {
			closeErr = err
		}
		w.gz = nil
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		w.file = nil
	}
	if closeErr != nil {
		return fmt.Errorf("close %s shard: %w", w.table, closeErr)
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func exportValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func importValue(value any) any {
	switch v := value.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(v.String(), 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(v.String(), 64); err == nil {
			return f
		}
		return v.String()
	default:
		return v
	}
}

func insertSQL(table string, columns []string) string {
	quoted := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = quoteIdent(column)
		placeholders[i] = "?"
	}
	return "insert into " + quoteIdent(table) + "(" + strings.Join(quoted, ",") + ") values(" + strings.Join(placeholders, ",") + ")"
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func normalizeBranch(branch string) string {
	if strings.TrimSpace(branch) == "" {
		return defaultBranch
	}
	return strings.TrimSpace(branch)
}

func run(ctx context.Context, dir, name string, args ...string) error {
	out, err := output(ctx, dir, name, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}

func output(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	body, err := cmd.CombinedOutput()
	return string(body), err
}

func isNonFastForwardPush(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "non-fast-forward") ||
		strings.Contains(lower, "fetch first") ||
		strings.Contains(lower, "failed to push some refs")
}

func parseSyncTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}
