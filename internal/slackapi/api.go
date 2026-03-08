package slackapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

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
	UserAuthError     string `json:"user_auth_error,omitempty"`
	AppTailAvailable  bool   `json:"app_tail_available"`
}

type SyncOptions struct {
	WorkspaceID string
	Channels    []string
	Since       string
	Full        bool
	Concurrency int
}

type Client struct {
	bot          *slack.Client
	user         *slack.Client
	tokens       config.Tokens
	appToken     string
	sleep        func(context.Context, time.Duration) error
	now          func() time.Time
	socketModeFn func(*slack.Client) socketModeRunner
}

func New(tokens config.Tokens) *Client {
	return NewWithOptions(tokens, "", nil)
}

func NewWithOptions(tokens config.Tokens, apiURL string, httpClient *http.Client) *Client {
	client := &Client{
		tokens:   tokens,
		appToken: tokens.App,
		sleep:    sleepContext,
		now:      func() time.Time { return time.Now().UTC() },
	}

	buildOptions := func(includeAppToken bool) []slack.Option {
		var options []slack.Option
		if apiURL != "" {
			options = append(options, slack.OptionAPIURL(apiURL))
		}
		if httpClient != nil {
			options = append(options, slack.OptionHTTPClient(httpClient))
		}
		if includeAppToken && tokens.App != "" {
			options = append(options, slack.OptionAppLevelToken(tokens.App))
		}
		return options
	}

	if tokens.Bot != "" {
		client.bot = slack.New(tokens.Bot, buildOptions(true)...)
	}
	if tokens.User != "" {
		client.user = slack.New(tokens.User, buildOptions(false)...)
	}
	client.socketModeFn = func(api *slack.Client) socketModeRunner {
		return managedSocketMode{client: socketmode.New(api)}
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
	if c.bot == nil {
		return diag, nil
	}

	resp, err := c.authTest(ctx, c.bot)
	if err != nil {
		return diag, err
	}
	diag.BotAuthTeamID = resp.TeamID
	diag.BotAuthTeam = resp.Team
	diag.AppTailAvailable = c.tokens.App != ""

	if c.user != nil {
		if _, err := c.authTest(ctx, c.user); err == nil {
			diag.UserAuthAvailable = true
			diag.ThreadCoverage = "full"
		} else {
			diag.UserAuthError = authErrorReason(err)
		}
	}
	return diag, nil
}

func (c *Client) Sync(ctx context.Context, st *store.Store, opts SyncOptions) error {
	if c.bot == nil {
		return errors.New("SLACK_BOT_TOKEN is required for api sync")
	}

	auth, err := c.authTest(ctx, c.bot)
	if err != nil {
		return err
	}
	workspaceID := auth.TeamID
	if opts.WorkspaceID != "" {
		workspaceID = opts.WorkspaceID
	}

	now := c.now()
	if err := st.UpsertWorkspace(ctx, store.Workspace{
		ID:           workspaceID,
		Name:         auth.Team,
		EnterpriseID: auth.EnterpriseID,
		RawJSON:      store.MarshalRaw(auth),
		UpdatedAt:    now,
	}); err != nil {
		return err
	}
	userRepliesAvailable := c.userAuthAvailable(ctx)

	channels, err := c.fetchChannels(ctx, workspaceID)
	if err != nil {
		return err
	}
	allow := make(map[string]struct{}, len(opts.Channels))
	for _, id := range opts.Channels {
		allow[id] = struct{}{}
	}
	selectedChannels := make([]slack.Channel, 0, len(channels))
	for _, channel := range channels {
		if len(allow) > 0 {
			if _, ok := allow[channel.ID]; !ok {
				continue
			}
		}
		selectedChannels = append(selectedChannels, channel)
	}
	if err := c.syncChannels(ctx, st, workspaceID, selectedChannels, opts, now, userRepliesAvailable); err != nil {
		return err
	}

	users, err := c.getUsers(ctx, c.bot)
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := st.UpsertUser(ctx, toStoreUser(workspaceID, user, now)); err != nil {
			return err
		}
	}

	threadCoverage := "partial"
	if userRepliesAvailable {
		threadCoverage = "full"
	}
	if err := st.SetSyncState(ctx, "doctor", "threads", "coverage", threadCoverage); err != nil {
		return err
	}
	return st.SetSyncState(ctx, SourceBot, "workspace", workspaceID, now.Format(time.RFC3339))
}

func (c *Client) Tail(ctx context.Context, st *store.Store, workspaceID string, repairEvery time.Duration) error {
	if c.bot == nil {
		return errors.New("SLACK_BOT_TOKEN is required for tail")
	}
	if c.appToken == "" {
		return errors.New("SLACK_APP_TOKEN is required for tail")
	}

	auth, err := c.authTest(ctx, c.bot)
	if err != nil {
		return err
	}
	if workspaceID == "" {
		workspaceID = auth.TeamID
	}

	socketClient := c.socketModeFn(c.bot)
	errCh := make(chan error, 1)
	go func() {
		errCh <- socketClient.Run()
	}()

	var ticker *time.Ticker
	if repairEvery > 0 {
		ticker = time.NewTicker(repairEvery)
		defer ticker.Stop()
	}

	for {
		select {
		case err := <-errCh:
			return err
		case event := <-socketClient.Events():
			if err := c.handleSocketModeEvent(ctx, st, workspaceID, socketClient, event); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-tickerChan(ticker):
			if err := c.repairWorkspace(ctx, st, workspaceID); err != nil {
				return err
			}
			if err := st.SetSyncState(ctx, "tail", "repair", workspaceID, c.now().Format(time.RFC3339)); err != nil {
				return err
			}
		}
	}
}

func (c *Client) HandleEventsAPIEvent(ctx context.Context, st *store.Store, workspaceID string, event slackevents.EventsAPIEvent) error {
	now := c.now()
	switch ev := event.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		msg := messageFromEvent(ev)
		return st.UpsertMessage(ctx, toStoreMessage(workspaceID, msg, SourceBot, 2, now), toStoreMentions(msg))
	case *slackevents.ChannelRenameEvent:
		return st.RenameChannel(ctx, ev.Channel.ID, ev.Channel.Name)
	case *slackevents.ChannelArchiveEvent:
		return st.SetChannelArchived(ctx, ev.Channel, true)
	case *slackevents.ChannelUnarchiveEvent:
		return st.SetChannelArchived(ctx, ev.Channel, false)
	default:
		return nil
	}
}

func (c *Client) fetchChannels(ctx context.Context, workspaceID string) ([]slack.Channel, error) {
	var (
		cursor   string
		channels []slack.Channel
	)
	for {
		page, nextCursor, err := c.getConversations(ctx, &slack.GetConversationsParameters{
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

func (c *Client) syncChannelMessages(ctx context.Context, st *store.Store, workspaceID string, channel slack.Channel, oldest string, now time.Time, userRepliesAvailable bool) error {
	cursor := ""
	joined := false
	for {
		resp, err := c.getConversationHistory(ctx, c.bot, &slack.GetConversationHistoryParameters{
			ChannelID: channel.ID,
			Cursor:    cursor,
			Limit:     200,
			Oldest:    oldest,
		})
		if err != nil {
			if !joined && channelSkipReason(err) == "not_in_channel" && !channel.IsPrivate {
				joinErr := c.joinConversation(ctx, channel.ID)
				if joinErr == nil {
					joined = true
					if setErr := st.SetSyncState(ctx, SourceBot, "channel_join", channel.ID, "joined"); setErr != nil {
						return setErr
					}
					continue
				}
				if setErr := st.SetSyncState(ctx, SourceBot, "channel_join", channel.ID, "failed:"+authErrorReason(joinErr)); setErr != nil {
					return setErr
				}
			}
			if isChannelHistorySkipped(err) {
				return st.SetSyncState(ctx, SourceBot, "channel_skip", channel.ID, channelSkipReason(err))
			}
			return fmt.Errorf("channel %s history: %w", channel.ID, err)
		}
		for _, msg := range resp.Messages {
			if err := st.UpsertMessage(ctx, toStoreMessage(workspaceID, msg, SourceBot, 2, now), toStoreMentions(msg)); err != nil {
				return err
			}
			if msg.ReplyCount > 0 && userRepliesAvailable {
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
		msgs, _, nextCursor, err := c.getConversationReplies(ctx, &slack.GetConversationRepliesParameters{
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

func (c *Client) handleSocketModeEvent(ctx context.Context, st *store.Store, workspaceID string, socketClient socketModeRunner, event socketmode.Event) error {
	switch event.Type {
	case socketmode.EventTypeConnected:
		return st.SetSyncState(ctx, "tail", "connection", workspaceID, c.now().Format(time.RFC3339))
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return nil
		}
		if event.Request != nil {
			socketClient.Ack(*event.Request)
		}
		return c.HandleEventsAPIEvent(ctx, st, workspaceID, eventsAPIEvent)
	default:
		return nil
	}
}

func (c *Client) repairWorkspace(ctx context.Context, st *store.Store, workspaceID string) error {
	if c.bot == nil {
		return errors.New("SLACK_BOT_TOKEN is required for repair")
	}
	channels, err := c.fetchChannels(ctx, workspaceID)
	if err != nil {
		return err
	}
	cursors, err := st.ChannelSyncCursors(ctx, workspaceID)
	if err != nil {
		return err
	}
	latestByChannel := make(map[string]string, len(cursors))
	for _, cursor := range cursors {
		latestByChannel[cursor.ID] = cursor.LatestTS
	}
	now := c.now()
	for _, channel := range channels {
		repairSince := repairOldest(latestByChannel[channel.ID], time.Hour)
		if err := c.syncChannels(ctx, st, workspaceID, []slack.Channel{channel}, SyncOptions{Since: repairSince}, now, c.userAuthAvailable(ctx)); err != nil {
			return err
		}
	}
	return nil
}

func messageFromEvent(event *slackevents.MessageEvent) slack.Message {
	msg := slack.Message{}
	if event.Message != nil {
		msg.Msg = *event.Message
	}
	if event.PreviousMessage != nil {
		if msg.Text == "" {
			msg.Text = event.PreviousMessage.Text
		}
		if msg.Timestamp == "" {
			msg.Timestamp = event.PreviousMessage.Timestamp
		}
		if msg.ThreadTimestamp == "" {
			msg.ThreadTimestamp = event.PreviousMessage.ThreadTimestamp
		}
		if msg.User == "" {
			msg.User = event.PreviousMessage.User
		}
	}
	if msg.Channel == "" {
		msg.Channel = event.Channel
	}
	if msg.User == "" {
		msg.User = event.User
	}
	if msg.Text == "" {
		msg.Text = event.Text
	}
	if msg.Timestamp == "" {
		msg.Timestamp = event.TimeStamp
	}
	if msg.ThreadTimestamp == "" {
		msg.ThreadTimestamp = event.ThreadTimeStamp
	}
	if msg.SubType == "" {
		msg.SubType = event.SubType
	}
	if event.SubType == "message_deleted" && msg.DeletedTimestamp == "" {
		msg.DeletedTimestamp = event.DeletedTimeStamp
	}
	return msg
}

func (c *Client) authTest(ctx context.Context, client *slack.Client) (*slack.AuthTestResponse, error) {
	return retry(ctx, c.sleep, 3, func() (*slack.AuthTestResponse, error) {
		return client.AuthTestContext(ctx)
	})
}

func (c *Client) getConversations(ctx context.Context, params *slack.GetConversationsParameters) ([]slack.Channel, string, error) {
	type result struct {
		channels   []slack.Channel
		nextCursor string
	}
	res, err := retry(ctx, c.sleep, 3, func() (result, error) {
		channels, nextCursor, err := c.bot.GetConversationsContext(ctx, params)
		return result{channels: channels, nextCursor: nextCursor}, err
	})
	return res.channels, res.nextCursor, err
}

func (c *Client) getConversationHistory(ctx context.Context, client *slack.Client, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	return retry(ctx, c.sleep, 3, func() (*slack.GetConversationHistoryResponse, error) {
		return client.GetConversationHistoryContext(ctx, params)
	})
}

func (c *Client) getConversationReplies(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	type result struct {
		msgs       []slack.Message
		hasMore    bool
		nextCursor string
	}
	res, err := retry(ctx, c.sleep, 3, func() (result, error) {
		msgs, hasMore, nextCursor, err := c.user.GetConversationRepliesContext(ctx, params)
		return result{msgs: msgs, hasMore: hasMore, nextCursor: nextCursor}, err
	})
	return res.msgs, res.hasMore, res.nextCursor, err
}

func (c *Client) getUsers(ctx context.Context, client *slack.Client) ([]slack.User, error) {
	return retry(ctx, c.sleep, 3, func() ([]slack.User, error) {
		return client.GetUsersContext(ctx)
	})
}

func (c *Client) joinConversation(ctx context.Context, channelID string) error {
	if c.bot == nil {
		return errors.New("SLACK_BOT_TOKEN is required for join")
	}
	_, err := retry(ctx, c.sleep, 3, func() (struct{}, error) {
		_, _, _, err := c.bot.JoinConversationContext(ctx, channelID)
		return struct{}{}, err
	})
	return err
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

type socketModeRunner interface {
	Run() error
	Ack(req socketmode.Request, payload ...interface{})
	Events() <-chan socketmode.Event
}

type managedSocketMode struct {
	client *socketmode.Client
}

func (m managedSocketMode) Run() error { return m.client.Run() }
func (m managedSocketMode) Ack(req socketmode.Request, payload ...interface{}) {
	m.client.Ack(req, payload...)
}
func (m managedSocketMode) Events() <-chan socketmode.Event { return m.client.Events }

func retry[T any](ctx context.Context, sleeper func(context.Context, time.Duration) error, attempts int, fn func() (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < attempts; attempt++ {
		value, err := fn()
		if err == nil {
			return value, nil
		}
		var rateLimited *slack.RateLimitedError
		if !errors.As(err, &rateLimited) || attempt == attempts-1 {
			return zero, err
		}
		if err := sleeper(ctx, rateLimited.RetryAfter); err != nil {
			return zero, err
		}
	}
	return zero, errors.New("retry exhausted")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func repairOldest(latestTS string, overlap time.Duration) string {
	if latestTS == "" {
		return ""
	}
	parsed, err := strconv.ParseFloat(latestTS, 64)
	if err != nil {
		return latestTS
	}
	adjusted := math.Max(parsed-overlap.Seconds(), 0)
	return strconv.FormatFloat(adjusted, 'f', 6, 64)
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

func (c *Client) syncChannels(ctx context.Context, st *store.Store, workspaceID string, channels []slack.Channel, opts SyncOptions, now time.Time, userRepliesAvailable bool) error {
	if len(channels) == 0 {
		return nil
	}
	oldestByChannel, err := c.channelOldestByID(ctx, st, workspaceID, channels, opts)
	if err != nil {
		return err
	}
	workerCount := opts.Concurrency
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(channels) {
		workerCount = len(channels)
	}
	if workerCount == 1 {
		for _, channel := range channels {
			if err := st.UpsertChannel(ctx, toStoreChannel(workspaceID, channel, now)); err != nil {
				return err
			}
			if err := c.syncChannelMessages(ctx, st, workspaceID, channel, oldestByChannel[channel.ID], now, userRepliesAvailable); err != nil {
				return err
			}
		}
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workCh := make(chan slack.Channel)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for channel := range workCh {
			if err := st.UpsertChannel(ctx, toStoreChannel(workspaceID, channel, now)); err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
				return
			}
			if err := c.syncChannelMessages(ctx, st, workspaceID, channel, oldestByChannel[channel.ID], now, userRepliesAvailable); err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}
	for _, channel := range channels {
		select {
		case <-ctx.Done():
			close(workCh)
			wg.Wait()
			select {
			case err := <-errCh:
				return err
			default:
				if ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
					return ctx.Err()
				}
				return nil
			}
		case workCh <- channel:
		}
	}
	close(workCh)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		return nil
	}
}

func (c *Client) channelOldestByID(ctx context.Context, st *store.Store, workspaceID string, channels []slack.Channel, opts SyncOptions) (map[string]string, error) {
	out := make(map[string]string, len(channels))
	if opts.Since != "" {
		for _, channel := range channels {
			out[channel.ID] = opts.Since
		}
		return out, nil
	}
	if opts.Full {
		return out, nil
	}

	cursors, err := st.ChannelSyncCursors(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	latestByChannel := make(map[string]string, len(cursors))
	for _, cursor := range cursors {
		latestByChannel[cursor.ID] = cursor.LatestTS
	}
	for _, channel := range channels {
		out[channel.ID] = repairOldest(latestByChannel[channel.ID], time.Hour)
	}
	return out, nil
}

func isChannelHistorySkipped(err error) bool {
	reason := channelSkipReason(err)
	return reason == "not_in_channel" || reason == "channel_not_found"
}

func channelSkipReason(err error) string {
	var slackErr slack.SlackErrorResponse
	if errors.As(err, &slackErr) && slackErr.Err != "" {
		return slackErr.Err
	}
	return ""
}

func (c *Client) userAuthAvailable(ctx context.Context) bool {
	if c.user == nil {
		return false
	}
	_, err := c.authTest(ctx, c.user)
	return err == nil
}

func authErrorReason(err error) string {
	if reason := channelSkipReason(err); reason != "" {
		return reason
	}
	return err.Error()
}
