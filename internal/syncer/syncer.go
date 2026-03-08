package syncer

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vincentkoc/slacrawl/internal/config"
	"github.com/vincentkoc/slacrawl/internal/slackapi"
	"github.com/vincentkoc/slacrawl/internal/slackdesktop"
	"github.com/vincentkoc/slacrawl/internal/store"
)

type Source string

const (
	SourceAPI     Source = "api"
	SourceDesktop Source = "desktop"
	SourceAll     Source = "all"
)

type Options struct {
	Source      Source
	WorkspaceID string
	Channels    []string
	Since       string
	Full        bool
}

type Summary struct {
	Desktop slackdesktop.Source `json:"desktop"`
}

func Run(ctx context.Context, cfg config.Config, st *store.Store, opts Options) (Summary, error) {
	tokens := cfg.ResolveTokens()
	summary := Summary{}

	switch opts.Source {
	case SourceAPI:
		return summary, slackapi.New(tokens).Sync(ctx, st, slackapi.SyncOptions{
			WorkspaceID: opts.WorkspaceID,
			Channels:    opts.Channels,
			Since:       opts.Since,
			Full:        opts.Full,
		})
	case SourceDesktop:
		return syncDesktop(ctx, cfg, st)
	case SourceAll:
		if err := slackapi.New(tokens).Sync(ctx, st, slackapi.SyncOptions{
			WorkspaceID: opts.WorkspaceID,
			Channels:    opts.Channels,
			Since:       opts.Since,
			Full:        opts.Full,
		}); err != nil {
			return summary, err
		}
		return syncDesktop(ctx, cfg, st)
	default:
		return summary, errors.New("unsupported source")
	}
}

func syncDesktop(ctx context.Context, cfg config.Config, st *store.Store) (Summary, error) {
	source, err := slackdesktop.Discover(cfg.Slack.Desktop.Path)
	if err != nil {
		return Summary{}, err
	}
	if !source.Available {
		return Summary{Desktop: source}, nil
	}
	if err := st.SetSyncState(ctx, "desktop", "root_state", "path", source.Path); err != nil {
		return Summary{}, err
	}
	payload := []byte(strings.Join(source.Summary.AppTeamsKeys, ","))
	if err := st.SetSyncState(ctx, "desktop", "root_state", "app_teams", string(payload)); err != nil {
		return Summary{}, err
	}
	if err := st.SetSyncState(ctx, "desktop", "root_state", "scanned_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return Summary{}, err
	}
	return Summary{Desktop: source}, nil
}
