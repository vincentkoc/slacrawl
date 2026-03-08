package slackdesktop

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/golang/snappy"
	"github.com/slack-go/slack"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/vincentkoc/slacrawl/internal/search"
	"github.com/vincentkoc/slacrawl/internal/store"
)

const (
	indexedDBBlobDir      = "IndexedDB/https_app.slack.com_0.indexeddb.blob"
	indexedDBSourceName   = "desktop-indexeddb"
	reduxPersistKeyPrefix = "persist:slack-client-"
)

var reduxV8Header = []byte{0xff, 0x0f}

//go:embed redux_decoder.js
var reduxDecoderScript string

type ReduxDecodedState struct {
	WorkspaceID string         `json:"workspace_id"`
	UserID      string         `json:"user_id"`
	Channels    []ReduxChannel `json:"channels"`
	Members     []ReduxMember  `json:"members"`
	Messages    []ReduxMessage `json:"messages"`
}

type ReduxChannel struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	IsChannel     bool          `json:"is_channel"`
	IsGroup       bool          `json:"is_group"`
	IsIM          bool          `json:"is_im"`
	IsMPIM        bool          `json:"is_mpim"`
	IsPrivate     bool          `json:"is_private"`
	IsArchived    bool          `json:"is_archived"`
	IsGeneral     bool          `json:"is_general"`
	ContextTeamID string        `json:"context_team_id"`
	Topic         ReduxTextMeta `json:"topic"`
	Purpose       ReduxTextMeta `json:"purpose"`
}

type ReduxTextMeta struct {
	Value string `json:"value"`
}

type ReduxMember struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	IsBot   bool               `json:"is_bot"`
	Deleted bool               `json:"deleted"`
	TeamID  string             `json:"team_id"`
	Real    string             `json:"real_name"`
	Profile ReduxMemberProfile `json:"profile"`
}

type ReduxMemberProfile struct {
	RealName    string `json:"real_name"`
	DisplayName string `json:"display_name"`
	Title       string `json:"title"`
}

type ReduxMessage struct {
	Channel      string       `json:"channel"`
	TS           string       `json:"ts"`
	ThreadTS     string       `json:"thread_ts"`
	User         string       `json:"user"`
	Subtype      string       `json:"subtype"`
	Type         string       `json:"type"`
	ClientMsgID  string       `json:"client_msg_id"`
	ParentUserID string       `json:"parent_user_id"`
	Text         string       `json:"text"`
	ReplyCount   int          `json:"reply_count"`
	LatestReply  string       `json:"latest_reply"`
	Hidden       bool         `json:"hidden"`
	Edited       *ReduxEdited `json:"edited"`
}

type ReduxEdited struct {
	TS string `json:"ts"`
}

type reduxBlobRef struct {
	WorkspaceID string
	UserID      string
	DirToken    byte
	FileToken   byte
}

func ExtractIndexedDBStates(path string) ([]ReduxDecodedState, error) {
	if _, err := exec.LookPath("node"); err != nil {
		return nil, nil
	}

	refs, err := parseReduxBlobRefs(filepath.Join(path, indexedDBDir), filepath.Join(path, indexedDBBlobDir))
	if err != nil {
		return nil, err
	}

	states := make([]ReduxDecodedState, 0, len(refs))
	for _, ref := range refs {
		state, err := decodeReduxBlob(ref.blobPath(filepath.Join(path, indexedDBBlobDir)))
		if err != nil {
			continue
		}
		state.WorkspaceID = ref.WorkspaceID
		state.UserID = ref.UserID
		states = append(states, state)
	}
	return states, nil
}

func ingestReduxStates(ctx context.Context, st *store.Store, states []ReduxDecodedState, now time.Time) error {
	for _, state := range states {
		if state.WorkspaceID == "" {
			continue
		}
		if err := st.UpsertWorkspace(ctx, store.Workspace{
			ID:        state.WorkspaceID,
			Name:      state.WorkspaceID,
			RawJSON:   store.MarshalRaw(map[string]any{"workspace_id": state.WorkspaceID, "source": indexedDBSourceName}),
			UpdatedAt: now,
		}); err != nil {
			return err
		}
		for _, member := range state.Members {
			workspaceID := fallback(member.TeamID, state.WorkspaceID)
			if err := st.UpsertUser(ctx, store.User{
				ID:          member.ID,
				WorkspaceID: workspaceID,
				Name:        fallback(member.Name, member.ID),
				RealName:    fallback(member.Profile.RealName, member.Real),
				DisplayName: member.Profile.DisplayName,
				Title:       member.Profile.Title,
				IsBot:       member.IsBot,
				IsDeleted:   member.Deleted,
				RawJSON:     store.MarshalRaw(member),
				UpdatedAt:   now,
			}); err != nil {
				return err
			}
		}
		allowedChannels := map[string]struct{}{}
		blockedChannels := map[string]struct{}{}
		for _, channel := range state.Channels {
			if channel.IsIM || channel.IsMPIM {
				blockedChannels[channel.ID] = struct{}{}
				continue
			}
			workspaceID := fallback(channel.ContextTeamID, state.WorkspaceID)
			if err := st.UpsertChannel(ctx, store.Channel{
				ID:          channel.ID,
				WorkspaceID: workspaceID,
				Name:        fallback(channel.Name, channel.ID),
				Kind:        reduxChannelKind(channel),
				Topic:       channel.Topic.Value,
				Purpose:     channel.Purpose.Value,
				IsPrivate:   channel.IsPrivate || channel.IsGroup,
				IsArchived:  channel.IsArchived,
				IsGeneral:   channel.IsGeneral,
				RawJSON:     store.MarshalRaw(channel),
				UpdatedAt:   now,
			}); err != nil {
				return err
			}
			allowedChannels[channel.ID] = struct{}{}
		}
		for _, message := range state.Messages {
			if message.Channel == "" || message.TS == "" {
				continue
			}
			if _, blocked := blockedChannels[message.Channel]; blocked {
				continue
			}
			if _, ok := allowedChannels[message.Channel]; !ok && !strings.HasPrefix(message.Channel, "C") && !strings.HasPrefix(message.Channel, "G") {
				continue
			}
			text := strings.TrimSpace(message.Text)
			if text == "" && message.Subtype == "" && message.Type == "" {
				continue
			}
			if err := st.UpsertMessage(ctx, store.Message{
				ChannelID:      message.Channel,
				TS:             message.TS,
				WorkspaceID:    state.WorkspaceID,
				UserID:         message.User,
				Subtype:        message.Subtype,
				ClientMsgID:    message.ClientMsgID,
				ThreadTS:       message.ThreadTS,
				ParentUserID:   message.ParentUserID,
				Text:           text,
				NormalizedText: normalizeReduxMessage(message),
				ReplyCount:     message.ReplyCount,
				LatestReply:    message.LatestReply,
				EditedTS:       editedTS(message),
				SourceRank:     3,
				SourceName:     indexedDBSourceName,
				RawJSON:        store.MarshalRaw(message),
				UpdatedAt:      now,
			}, reduxMentions(message.Text)); err != nil {
				return err
			}
		}
		if err := st.SetSyncState(ctx, sourceName, "indexeddb", "decoded_states", fmt.Sprintf("%d", len(states))); err != nil {
			return err
		}
	}
	return nil
}

func parseReduxBlobRefs(indexedDBPath string, blobRoot string) ([]reduxBlobRef, error) {
	db, err := leveldb.OpenFile(indexedDBPath, &opt.Options{ReadOnly: true, Comparer: indexedDBComparer{}})
	if err != nil {
		return nil, err
	}
	defer db.Close()

	refs := map[string]*reduxBlobRef{}
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		dbid, osid, idx, rest, ok := decodeIndexedDBPrefix(iter.Key())
		if !ok || dbid != 1 || osid != 1 {
			continue
		}
		key := decodeIndexedDBStringKey(rest)
		if !strings.HasPrefix(key, reduxPersistKeyPrefix) {
			continue
		}
		workspaceID, userID, ok := parseReduxPersistKey(key)
		if !ok {
			continue
		}
		ref := refs[key]
		if ref == nil {
			ref = &reduxBlobRef{WorkspaceID: workspaceID, UserID: userID}
			refs[key] = ref
		}
		switch idx {
		case 2:
			if len(iter.Value()) >= 2 {
				ref.DirToken = iter.Value()[1]
			}
		case 3:
			if len(iter.Value()) >= 2 {
				ref.FileToken = iter.Value()[1]
			}
		}
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}

	out := make([]reduxBlobRef, 0, len(refs))
	for _, ref := range refs {
		if ref.DirToken == 0 || ref.FileToken == 0 {
			continue
		}
		if _, err := os.Stat(ref.blobPath(blobRoot)); err != nil {
			continue
		}
		out = append(out, *ref)
	}
	return out, nil
}

func decodeReduxBlob(blobPath string) (ReduxDecodedState, error) {
	raw, err := os.ReadFile(blobPath)
	if err != nil {
		return ReduxDecodedState{}, err
	}

	decoded := raw
	if len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0x11 && raw[2] == 0x02 {
		decoded, err = snappy.Decode(nil, raw[3:])
		if err != nil {
			return ReduxDecodedState{}, err
		}
	}

	offset := bytes.Index(decoded, reduxV8Header)
	if offset < 0 {
		return ReduxDecodedState{}, fmt.Errorf("v8 payload not found in %s", blobPath)
	}

	tempFile, err := os.CreateTemp("", "slacrawl-redux-*.bin")
	if err != nil {
		return ReduxDecodedState{}, err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := tempFile.Write(decoded[offset:]); err != nil {
		_ = tempFile.Close()
		return ReduxDecodedState{}, err
	}
	if err := tempFile.Close(); err != nil {
		return ReduxDecodedState{}, err
	}

	cmd := exec.Command("node", "-e", reduxDecoderScript, tempPath)
	output, err := cmd.Output()
	if err != nil {
		return ReduxDecodedState{}, err
	}

	var state ReduxDecodedState
	if err := json.Unmarshal(output, &state); err != nil {
		return ReduxDecodedState{}, err
	}
	return state, nil
}

func decodeIndexedDBPrefix(key []byte) (dbid int, objectStoreID int, indexID int, rest []byte, ok bool) {
	if len(key) < 1 {
		return 0, 0, 0, nil, false
	}
	header := key[0]
	dbLen := int((header>>5)&0x07) + 1
	storeLen := int((header>>2)&0x07) + 1
	indexLen := int(header&0x03) + 1
	need := 1 + dbLen + storeLen + indexLen
	if len(key) < need {
		return 0, 0, 0, nil, false
	}

	offset := 1
	read := func(size int) int {
		value := 0
		for i := 0; i < size; i++ {
			value |= int(key[offset+i]) << (8 * i)
		}
		offset += size
		return value
	}

	dbid = read(dbLen)
	objectStoreID = read(storeLen)
	indexID = read(indexLen)
	return dbid, objectStoreID, indexID, key[need:], true
}

func decodeIndexedDBStringKey(key []byte) string {
	if len(key) == 0 || key[0] != 0x01 {
		return ""
	}
	length, used := decodeVarInt(key[1:])
	if used == 0 {
		return ""
	}
	raw := key[1+used:]
	if len(raw) < length*2 {
		return ""
	}
	data := make([]uint16, 0, length)
	for i := 0; i < length*2; i += 2 {
		data = append(data, uint16(raw[i])<<8|uint16(raw[i+1]))
	}
	return string(utf16.Decode(data))
}

func parseReduxPersistKey(key string) (workspaceID string, userID string, ok bool) {
	if !strings.HasPrefix(key, reduxPersistKeyPrefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(key, reduxPersistKeyPrefix), "-")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func decodeVarInt(data []byte) (int, int) {
	value := 0
	shift := 0
	for index, b := range data {
		value |= int(b&0x7f) << shift
		if b < 0x80 {
			return value, index + 1
		}
		shift += 7
	}
	return 0, 0
}

func reduxChannelKind(channel ReduxChannel) string {
	switch {
	case channel.IsIM:
		return "desktop_im"
	case channel.IsMPIM:
		return "desktop_mpim"
	case channel.IsGroup || channel.IsPrivate:
		return "desktop_private_channel"
	default:
		return "desktop_channel"
	}
}

func normalizeReduxMessage(message ReduxMessage) string {
	slackMessage := slack.Message{
		Msg: slack.Msg{
			Text:            message.Text,
			Timestamp:       message.TS,
			ThreadTimestamp: message.ThreadTS,
			SubType:         message.Subtype,
			DeletedTimestamp: func() string {
				if message.Hidden && message.Subtype == "tombstone" {
					return message.TS
				}
				return ""
			}(),
		},
	}
	return search.NormalizeMessage(slackMessage)
}

func reduxMentions(text string) []store.Mention {
	raw := search.ExtractMentions(text)
	mentions := make([]store.Mention, 0, len(raw))
	seen := map[string]struct{}{}
	for _, mention := range raw {
		key := mention.Type + "|" + mention.TargetID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		mentions = append(mentions, store.Mention{
			Type:        mention.Type,
			TargetID:    mention.TargetID,
			DisplayText: mention.DisplayText,
		})
	}
	return mentions
}

func editedTS(message ReduxMessage) string {
	if message.Edited == nil {
		return ""
	}
	return message.Edited.TS
}

func (r reduxBlobRef) blobPath(blobRoot string) string {
	dir := fmt.Sprintf("%02x", r.DirToken)
	file := fmt.Sprintf("%s%02x", dir, r.FileToken)
	return filepath.Join(blobRoot, "1", dir, file)
}
