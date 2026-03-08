package slackapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/slack-go/slack"

	"github.com/vincentkoc/slacrawl/internal/config"
	"github.com/vincentkoc/slacrawl/internal/search"
	"github.com/vincentkoc/slacrawl/internal/store"
)

const (
	SourceUser = "api-user"
	SourceBot  = "api-bot"
)

type Diagnostics struct {
	BotConfigured     bool   `json:"bot_configured"`
	AppConfigured     bool   `json:"app_configured"`
	UserConfigured    bool   `json:"user_configured"`
	ThreadCoverage    string `json:"thread_coverage"`
	BotAuthTeamID     string `json:"bot_auth_team_id,omitempty"`
	BotAuthTeam       string `json:"bot_auth_team,omitempty"`
	UserAuthAvailable bool   `json:"user_auth_available"`
	AppTailAvailable  bool   `json:"app_tail_available"`
}

type SyncOptions struct {
	WorkspaceID string
	Channels    []string
	Since       string
	Full        bool
}

type Client struct {
	bot      *slack.Client
	user     *slack.Client
	tokens   config.Tokens
	appToken string
}

func New(tokens config.Tokens) *Client {
	client := &Client{
		tokens:   tokens,
		appToken: tokens.App,
	}
	if tokens.Bot != "" {
		client.bot = slack.New(tokens.Bot)
	}
	if tokens.User != "" {
		client.user = slack.New(tokens.User)
	}
	return client
}

func (c *Client) Doctor(ctx context.Context) (Diagnostics, error) {
	diag := Diagnostics{
		BotConfigured:  c.tokens.Bot != "",
		AppConfigured:  c.tokens.App != "",
		UserConfigured: c.tokens.User != "",
		ThreadCoverage: "partial",
	}
	if c.tokens.User != "" {
		diag.ThreadCoverage = "full"
	}
	if c.bot == nil {
		return diag, nil
	}

	resp, err := c.bot.AuthTestContext(ctx)
	if err != nil {
		return diag, err
	}
	diag.BotAuthTeamID = resp.TeamID
	diag.BotAuthTeam = resp.Team
	diag.AppTailAvailable = c.tokens.App != ""

	if c.user != nil {
		if _, err := c.user.AuthTestContext(ctx); err == nil {
			diag.UserAuthAvailable = true
		}
	}
	return diag, nil
}

func (c *Client) Sync(ctx context.Context, st *store.Store, opts SyncOptions) error {
	if c.bot == nil {
		return errors.New("SLACK_BOT_TOKEN is required for api sync")
	}

	auth, err := c.bot.AuthTestContext(ctx)
	if err != nil {
		return err
	}
	workspaceID := auth.TeamID
	if opts.WorkspaceID != "" {
		workspaceID = opts.WorkspaceID
	}

	now := time.Now().UTC()
	if err := st.UpsertWorkspace(ctx, store.Workspace{
		ID:           workspaceID,
		Name:         auth.Team,
		EnterpriseID: auth.EnterpriseID,
		RawJSON:      store.MarshalRaw(auth),
		UpdatedAt:    now,
	}); err != nil {
		return err
	}

	channels, err := c.fetchChannels(ctx, workspaceID)
	if err != nil {
		return err
	}
	allow := make(map[string]struct{}, len(opts.Channels))
	for _, id := range opts.Channels {
		allow[id] = struct{}{}
	}
	for _, channel := range channels {
		if len(allow) > 0 {
			if _, ok := allow[channel.ID]; !ok {
				continue
			}
		}
		if err := st.UpsertChannel(ctx, toStoreChannel(workspaceID, channel, now)); err != nil {
			return err
		}
		if err := c.syncChannelMessages(ctx, st, workspaceID, channel, opts, now); err != nil {
			return err
		}
	}

	users, err := c.bot.GetUsersContext(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := st.UpsertUser(ctx, toStoreUser(workspaceID, user, now)); err != nil {
			return err
		}
	}

	threadCoverage := "partial"
	if c.user != nil {
		threadCoverage = "full"
	}
	if err := st.SetSyncState(ctx, "doctor", "threads", "coverage", threadCoverage); err != nil {
		return err
	}
	return st.SetSyncState(ctx, SourceBot, "workspace", workspaceID, now.Format(time.RFC3339))
}

func (c *Client) fetchChannels(ctx context.Context, workspaceID string) ([]slack.Channel, error) {
	var (
		cursor   string
		channels []slack.Channel
	)
	for {
		page, nextCursor, err := c.bot.GetConversationsContext(ctx, &slack.GetConversationsParameters{
			Cursor:          cursor,
			ExcludeArchived: false,
			Limit:           200,
			Types:           []string{"public_channel", "private_channel"},
			TeamID:          workspaceID,
		})
		if err != nil {
			return nil, err
		}
		channels = append(channels, page...)
		if nextCursor == "" {
			return channels, nil
		}
		cursor = nextCursor
	}
}

func (c *Client) syncChannelMessages(ctx context.Context, st *store.Store, workspaceID string, channel slack.Channel, opts SyncOptions, now time.Time) error {
	cursor := ""
	for {
		resp, err := c.bot.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
			ChannelID: channel.ID,
			Cursor:    cursor,
			Limit:     200,
			Oldest:    opts.Since,
		})
		if err != nil {
			return fmt.Errorf("channel %s history: %w", channel.ID, err)
		}
		for _, msg := range resp.Messages {
			if err := st.UpsertMessage(ctx, toStoreMessage(workspaceID, msg, SourceBot, 2, now), toStoreMentions(msg)); err != nil {
				return err
			}
			if msg.ReplyCount > 0 && c.user != nil {
				if err := c.syncThread(ctx, st, workspaceID, channel.ID, msg.Timestamp, now); err != nil {
					return err
				}
			}
		}
		if resp.ResponseMetaData.NextCursor == "" {
			return nil
		}
		cursor = resp.ResponseMetaData.NextCursor
	}
}

func (c *Client) syncThread(ctx context.Context, st *store.Store, workspaceID string, channelID string, threadTS string, now time.Time) error {
	cursor := ""
	for {
		msgs, _, nextCursor, err := c.user.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Cursor:    cursor,
			Limit:     200,
		})
		if err != nil {
			return err
		}
		for _, msg := range msgs {
			if err := st.UpsertMessage(ctx, toStoreMessage(workspaceID, msg, SourceUser, 1, now), toStoreMentions(msg)); err != nil {
				return err
			}
		}
		if nextCursor == "" {
			return nil
		}
		cursor = nextCursor
	}
}

func toStoreChannel(workspaceID string, channel slack.Channel, now time.Time) store.Channel {
	kind := "public_channel"
	if channel.IsPrivate {
		kind = "private_channel"
	}
	return store.Channel{
		ID:          channel.ID,
		WorkspaceID: workspaceID,
		Name:        channel.Name,
		Kind:        kind,
		Topic:       channel.Topic.Value,
		Purpose:     channel.Purpose.Value,
		IsPrivate:   channel.IsPrivate,
		IsArchived:  channel.IsArchived,
		IsShared:    channel.IsShared,
		IsGeneral:   channel.IsGeneral,
		RawJSON:     store.MarshalRaw(channel),
		UpdatedAt:   now,
	}
}

func toStoreUser(workspaceID string, user slack.User, now time.Time) store.User {
	return store.User{
		ID:          user.ID,
		WorkspaceID: workspaceID,
		Name:        user.Name,
		RealName:    user.RealName,
		DisplayName: user.Profile.DisplayName,
		Title:       user.Profile.Title,
		IsBot:       user.IsBot,
		IsDeleted:   user.Deleted,
		RawJSON:     store.MarshalRaw(user),
		UpdatedAt:   now,
	}
}

func toStoreMessage(workspaceID string, msg slack.Message, sourceName string, sourceRank int, now time.Time) store.Message {
	editedTS := ""
	if msg.Edited != nil {
		editedTS = msg.Edited.Timestamp
	}
	return store.Message{
		ChannelID:      msg.Channel,
		TS:             msg.Timestamp,
		WorkspaceID:    workspaceID,
		UserID:         msg.User,
		Subtype:        msg.SubType,
		ClientMsgID:    msg.ClientMsgID,
		ThreadTS:       msg.ThreadTimestamp,
		ParentUserID:   msg.ParentUserId,
		Text:           msg.Text,
		NormalizedText: search.NormalizeMessage(msg),
		ReplyCount:     msg.ReplyCount,
		LatestReply:    msg.LatestReply,
		EditedTS:       editedTS,
		DeletedTS:      msg.DeletedTimestamp,
		SourceRank:     sourceRank,
		SourceName:     sourceName,
		RawJSON:        store.MarshalRaw(msg),
		UpdatedAt:      now,
	}
}

func toStoreMentions(msg slack.Message) []store.Mention {
	raw := search.ExtractMentions(msg.Text)
	mentions := make([]store.Mention, 0, len(raw))
	for _, mention := range raw {
		mentions = append(mentions, store.Mention{
			Type:        mention.Type,
			TargetID:    mention.TargetID,
			DisplayText: mention.DisplayText,
		})
	}
	return mentions
}
