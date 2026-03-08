package slackdesktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/comparer"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/vincentkoc/slacrawl/internal/config"
	"github.com/vincentkoc/slacrawl/internal/store"
)

const (
	localStorageDir = "Local Storage/leveldb"
	indexedDBDir    = "IndexedDB/https_app.slack.com_0.indexeddb.leveldb"
	rootStateFile   = "storage/root-state.json"
	sourceName      = "desktop"
	draftSourceName = "desktop-draft"
)

type Source struct {
	Path      string              `json:"path"`
	Available bool                `json:"available"`
	Summary   RootStateSummary    `json:"summary"`
	Local     LocalStorageSummary `json:"local_storage"`
	IndexedDB IndexedDBSummary    `json:"indexeddb"`
	Snapshot  string              `json:"snapshot_path,omitempty"`
}

type RootStateSummary struct {
	AppTeamsKeys      []string `json:"app_teams_keys"`
	WorkspaceCount    int      `json:"workspace_count"`
	TeamsCount        int      `json:"teams_count"`
	DownloadTeamCount int      `json:"download_team_count"`
	DownloadItemCount int      `json:"download_item_count"`
}

type LocalStorageSummary struct {
	WorkspaceCount     int `json:"workspace_count"`
	DraftCount         int `json:"draft_count"`
	ActivityTeamCount  int `json:"activity_team_count"`
	RecentChannelCount int `json:"recent_channel_count"`
}

type IndexedDBSummary struct {
	ObjectStores []string `json:"object_stores"`
}

type Snapshot struct {
	Root string
}

type ExtractedData struct {
	RootState   RootStateData
	LocalConfig LocalConfig
	Drafts      []Draft
	Activity    map[string]ActivitySession
	Recent      map[string][]string
	IndexedDB   IndexedDBSummary
}

type RootStateData struct {
	Summary   RootStateSummary
	Downloads map[string]map[string]DownloadRecord
}

type DownloadRecord struct {
	ID         string `json:"id"`
	TeamID     string `json:"teamId"`
	UserID     string `json:"userId"`
	URL        string `json:"url"`
	AppVersion string `json:"appVersion"`
	State      string `json:"downloadState"`
	Path       string `json:"downloadPath"`
}

type rootState struct {
	AppTeams   map[string]json.RawMessage           `json:"appTeams"`
	Downloads  map[string]map[string]DownloadRecord `json:"downloads"`
	Workspaces map[string]json.RawMessage           `json:"workspaces"`
	Teams      map[string]json.RawMessage           `json:"teams"`
}

type LocalConfig struct {
	Teams map[string]DesktopTeam `json:"teams"`
}

type DesktopTeam struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	URL        string      `json:"url"`
	Domain     string      `json:"domain"`
	Token      string      `json:"token,omitempty"`
	UserID     string      `json:"user_id"`
	UserLocale string      `json:"user_locale"`
	Icon       interface{} `json:"icon,omitempty"`
}

type DraftsState struct {
	UnifiedDrafts map[string]Draft `json:"unifiedDrafts"`
}

type Draft struct {
	ID             string             `json:"id"`
	ClientDraftID  string             `json:"client_draft_id"`
	IsFromComposer bool               `json:"is_from_composer"`
	DateCreated    float64            `json:"date_created"`
	LastUpdated    float64            `json:"last_updated"`
	LastUpdatedTS  float64            `json:"last_updated_ts"`
	Destinations   []DraftDestination `json:"destinations"`
	Ops            []DraftOp          `json:"ops"`
	FileIDs        []string           `json:"file_ids"`
}

type DraftDestination struct {
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"thread_ts"`
	Broadcast bool   `json:"broadcast"`
}

type DraftOp struct {
	Insert     interface{}            `json:"insert"`
	Attributes map[string]interface{} `json:"attributes"`
}

type ActivitySession map[string]ActivityRecord

type ActivityRecord struct {
	ID           string `json:"id"`
	StartTime    int64  `json:"startTime"`
	LastActivity int64  `json:"lastActivity"`
	LastLogged   int64  `json:"lastLogged"`
}

func Discover(path string) (Source, error) {
	if path == "" {
		return Source{}, errors.New("desktop path missing")
	}
	source := Source{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return source, nil
		}
		return Source{}, err
	}
	if !info.IsDir() {
		return Source{}, errors.New("desktop path is not a directory")
	}
	source.Available = true

	root, err := LoadRootState(filepath.Join(path, rootStateFile))
	if err != nil && !os.IsNotExist(err) {
		return Source{}, err
	}
	source.Summary = root.Summary

	local, err := ParseLocalStorage(filepath.Join(path, localStorageDir))
	if err != nil && !os.IsNotExist(err) {
		return Source{}, err
	}
	source.Local = local.Summary

	indexed, err := ScanIndexedDB(filepath.Join(path, indexedDBDir))
	if err != nil && !os.IsNotExist(err) {
		return Source{}, err
	}
	source.IndexedDB = indexed
	return source, nil
}

func SnapshotPath(path string) (Snapshot, error) {
	root, err := os.MkdirTemp("", "slacrawl-desktop-*")
	if err != nil {
		return Snapshot{}, err
	}

	target := filepath.Join(root, "Slack")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Snapshot{}, err
	}

	copyTargets := []string{
		rootStateFile,
		"local-settings.json",
		localStorageDir,
		indexedDBDir,
	}
	for _, relative := range copyTargets {
		src := filepath.Join(path, filepath.FromSlash(relative))
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Snapshot{}, err
		}
		dst := filepath.Join(target, filepath.FromSlash(relative))
		if err := copyPath(src, dst); err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{Root: target}, nil
}

func Extract(path string) (ExtractedData, error) {
	root, err := LoadRootState(filepath.Join(path, rootStateFile))
	if err != nil && !os.IsNotExist(err) {
		return ExtractedData{}, err
	}

	local, err := ParseLocalStorage(filepath.Join(path, localStorageDir))
	if err != nil && !os.IsNotExist(err) {
		return ExtractedData{}, err
	}

	indexed, err := ScanIndexedDB(filepath.Join(path, indexedDBDir))
	if err != nil && !os.IsNotExist(err) {
		return ExtractedData{}, err
	}

	return ExtractedData{
		RootState:   root,
		LocalConfig: local.LocalConfig,
		Drafts:      local.Drafts,
		Activity:    local.Activity,
		Recent:      local.Recent,
		IndexedDB:   indexed,
	}, nil
}

func Ingest(ctx context.Context, st *store.Store, sourcePath string) (Source, error) {
	source, err := Discover(sourcePath)
	if err != nil {
		return Source{}, err
	}
	if !source.Available {
		return source, nil
	}

	snapshot, err := SnapshotPath(sourcePath)
	if err != nil {
		return Source{}, err
	}
	source.Snapshot = snapshot.Root

	extracted, err := Extract(snapshot.Root)
	if err != nil {
		return Source{}, err
	}

	now := time.Now().UTC()
	for teamID, team := range extracted.LocalConfig.Teams {
		sanitized := team
		sanitized.Token = config.Redact(sanitized.Token)
		if err := st.UpsertWorkspace(ctx, store.Workspace{
			ID:        teamID,
			Name:      fallback(sanitized.Name, teamID),
			Domain:    sanitized.Domain,
			RawJSON:   store.MarshalRaw(sanitized),
			UpdatedAt: now,
		}); err != nil {
			return Source{}, err
		}
		if team.UserID != "" {
			if err := st.UpsertUser(ctx, store.User{
				ID:          team.UserID,
				WorkspaceID: teamID,
				Name:        fallback(team.UserID, team.UserID),
				DisplayName: fallback(team.Name, team.UserID),
				Title:       "desktop_local_user",
				RawJSON:     store.MarshalRaw(sanitized),
				UpdatedAt:   now,
			}); err != nil {
				return Source{}, err
			}
		}
	}

	for _, draft := range extracted.Drafts {
		if len(draft.Destinations) == 0 {
			continue
		}
		channelID := draft.Destinations[0].ChannelID
		workspaceID := workspaceForDraft(extracted.LocalConfig.Teams, channelID, draft)
		if workspaceID == "" {
			workspaceID = firstWorkspaceID(extracted.LocalConfig.Teams)
		}
		if workspaceID == "" {
			continue
		}

		if err := st.UpsertChannel(ctx, store.Channel{
			ID:          channelID,
			WorkspaceID: workspaceID,
			Name:        inferredChannelName(channelID, draft),
			Kind:        "desktop_unknown",
			RawJSON:     "{}",
			UpdatedAt:   now,
		}); err != nil {
			return Source{}, err
		}

		message := store.Message{
			ChannelID:      channelID,
			TS:             draftTS(draft),
			WorkspaceID:    workspaceID,
			UserID:         extracted.LocalConfig.Teams[workspaceID].UserID,
			Subtype:        "desktop_draft",
			ClientMsgID:    draft.ClientDraftID,
			ThreadTS:       draft.Destinations[0].ThreadTS,
			Text:           draftText(draft),
			NormalizedText: strings.TrimSpace(draftText(draft)),
			SourceRank:     3,
			SourceName:     draftSourceName,
			RawJSON:        store.MarshalRaw(draft),
			UpdatedAt:      now,
		}
		if message.Text == "" {
			continue
		}
		if err := st.UpsertMessage(ctx, message, nil); err != nil {
			return Source{}, err
		}
	}

	if err := st.SetSyncState(ctx, sourceName, "root_state", "path", source.Path); err != nil {
		return Source{}, err
	}
	if err := st.SetSyncState(ctx, sourceName, "root_state", "app_teams", strings.Join(source.Summary.AppTeamsKeys, ",")); err != nil {
		return Source{}, err
	}
	if err := st.SetSyncState(ctx, sourceName, "local_storage", "draft_count", intString(source.Local.DraftCount)); err != nil {
		return Source{}, err
	}
	if err := st.SetSyncState(ctx, sourceName, "indexeddb", "object_stores", strings.Join(source.IndexedDB.ObjectStores, ",")); err != nil {
		return Source{}, err
	}
	if err := st.SetSyncState(ctx, sourceName, "local_storage", "workspace_count", intString(source.Local.WorkspaceCount)); err != nil {
		return Source{}, err
	}
	if err := st.SetSyncState(ctx, sourceName, "local_storage", "activity_team_count", intString(source.Local.ActivityTeamCount)); err != nil {
		return Source{}, err
	}
	for teamID, downloads := range extracted.RootState.Downloads {
		if err := st.SetSyncState(ctx, sourceName, "downloads", teamID, intString(len(downloads))); err != nil {
			return Source{}, err
		}
	}

	return source, nil
}

func LoadRootState(path string) (RootStateData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RootStateData{}, err
	}

	var state rootState
	if err := json.Unmarshal(data, &state); err != nil {
		return RootStateData{}, err
	}

	keys := make([]string, 0, len(state.AppTeams))
	for key := range state.AppTeams {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	downloadItemCount := 0
	for _, teamDownloads := range state.Downloads {
		downloadItemCount += len(teamDownloads)
	}

	return RootStateData{
		Summary: RootStateSummary{
			AppTeamsKeys:      keys,
			WorkspaceCount:    len(state.Workspaces),
			TeamsCount:        len(state.Teams),
			DownloadTeamCount: len(state.Downloads),
			DownloadItemCount: downloadItemCount,
		},
		Downloads: state.Downloads,
	}, nil
}

type localStorageData struct {
	Summary     LocalStorageSummary
	LocalConfig LocalConfig
	Drafts      []Draft
	Activity    map[string]ActivitySession
	Recent      map[string][]string
}

func ParseLocalStorage(path string) (localStorageData, error) {
	db, err := leveldb.OpenFile(path, &opt.Options{ReadOnly: true})
	if err != nil {
		return localStorageData{}, err
	}
	defer db.Close()

	var (
		configData LocalConfig
		drafts     []Draft
		activity   = map[string]ActivitySession{}
		recent     = map[string][]string{}
	)

	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		key := cleanKey(iter.Key())
		if !strings.HasPrefix(key, "_https://app.slack.com") {
			continue
		}
		value := jsonPayload(iter.Value())
		if len(value) == 0 {
			continue
		}

		switch {
		case strings.Contains(key, "localConfig_v2"):
			var payload struct {
				Teams map[string]DesktopTeam `json:"teams"`
			}
			if err := json.Unmarshal(value, &payload); err == nil {
				configData.Teams = payload.Teams
			}
		case strings.Contains(key, "persist-v1::") && strings.HasSuffix(key, "::drafts"):
			var payload DraftsState
			if err := json.Unmarshal(value, &payload); err == nil {
				for id, draft := range payload.UnifiedDrafts {
					if draft.ClientDraftID == "" {
						draft.ClientDraftID = id
					}
					if draft.ID == "" {
						draft.ID = id
					}
					drafts = append(drafts, draft)
				}
			}
		case strings.Contains(key, "activitySession_"):
			teamID := strings.TrimPrefix(key, "_https://app.slack.comactivitySession_")
			var payload ActivitySession
			if err := json.Unmarshal(value, &payload); err == nil {
				activity[teamID] = payload
			}
		case strings.Contains(key, "persist-v1::") && strings.HasSuffix(key, "::recentlyJoinedChannels"):
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(value, &payload); err == nil {
				parts := strings.Split(key, "::")
				if len(parts) >= 2 {
					teamID := parts[1]
					for channelID := range payload {
						recent[teamID] = append(recent[teamID], channelID)
					}
				}
			}
		}
	}
	if err := iter.Error(); err != nil {
		return localStorageData{}, err
	}

	for teamID := range recent {
		sort.Strings(recent[teamID])
	}

	recentCount := 0
	for _, ids := range recent {
		recentCount += len(ids)
	}

	return localStorageData{
		Summary: LocalStorageSummary{
			WorkspaceCount:     len(configData.Teams),
			DraftCount:         len(drafts),
			ActivityTeamCount:  len(activity),
			RecentChannelCount: recentCount,
		},
		LocalConfig: configData,
		Drafts:      drafts,
		Activity:    activity,
		Recent:      recent,
	}, nil
}

func ScanIndexedDB(path string) (IndexedDBSummary, error) {
	db, err := leveldb.OpenFile(path, &opt.Options{ReadOnly: true, Comparer: indexedDBComparer{}})
	if err != nil {
		return IndexedDBSummary{}, err
	}
	defer db.Close()

	stores := map[string]struct{}{}
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		key := cleanKey(iter.Key())
		if !strings.Contains(key, "#objectStore-") {
			continue
		}
		idx := strings.Index(key, "#objectStore-")
		stores[key[idx+1:]] = struct{}{}
	}
	if err := iter.Error(); err != nil {
		return IndexedDBSummary{}, err
	}

	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	sort.Strings(names)
	return IndexedDBSummary{ObjectStores: names}, nil
}

type indexedDBComparer struct{}

func (indexedDBComparer) Compare(a, b []byte) int { return bytes.Compare(a, b) }
func (indexedDBComparer) Name() string            { return "idb_cmp1" }
func (indexedDBComparer) Separator(dst, a, b []byte) []byte {
	return comparer.DefaultComparer.Separator(dst, a, b)
}
func (indexedDBComparer) Successor(dst, b []byte) []byte {
	return comparer.DefaultComparer.Successor(dst, b)
}

func draftText(draft Draft) string {
	var builder strings.Builder
	for _, op := range draft.Ops {
		switch value := op.Insert.(type) {
		case string:
			builder.WriteString(value)
		default:
			continue
		}
	}
	return strings.TrimSpace(builder.String())
}

func draftTS(draft Draft) string {
	if draft.LastUpdatedTS > 0 {
		return "draft:" + trimFloat(draft.LastUpdatedTS) + ":" + fallback(draft.ClientDraftID, draft.ID)
	}
	if draft.LastUpdated > 0 {
		return "draft:" + trimFloat(draft.LastUpdated) + ":" + fallback(draft.ClientDraftID, draft.ID)
	}
	return "draft:" + fallback(draft.ClientDraftID, draft.ID)
}

func trimFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(jsonNumber(value)), " ", ""), "0"), ".")
}

func jsonNumber(value float64) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func firstWorkspaceID(teams map[string]DesktopTeam) string {
	ids := make([]string, 0, len(teams))
	for id := range teams {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func workspaceForDraft(teams map[string]DesktopTeam, channelID string, draft Draft) string {
	_ = channelID
	for workspaceID, team := range teams {
		if team.UserID != "" && hasDraftForWorkspace(workspaceID, draft) {
			return workspaceID
		}
	}
	return ""
}

func hasDraftForWorkspace(workspaceID string, draft Draft) bool {
	for _, destination := range draft.Destinations {
		if strings.HasPrefix(destination.ChannelID, "C") || strings.HasPrefix(destination.ChannelID, "D") || strings.HasPrefix(destination.ChannelID, "G") {
			return true
		}
		if strings.Contains(destination.ChannelID, workspaceID) {
			return true
		}
	}
	return false
}

func inferredChannelName(channelID string, draft Draft) string {
	if draft.Destinations != nil && len(draft.Destinations) > 0 && draft.Destinations[0].ThreadTS != "" {
		return channelID + " (thread)"
	}
	return channelID
}

func fallback(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func intString(value int) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func copyPath(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}

func cleanKey(key []byte) string {
	return strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, string(key))
}

func jsonPayload(value []byte) []byte {
	for i, b := range value {
		if b == '{' || b == '[' {
			return value[i:]
		}
	}
	return nil
}
