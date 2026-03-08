package syncer

import (
	"context"
	"errors"

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
	Concurrency int
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
			Concurrency: opts.Concurrency,
		})
	case SourceDesktop:
		return syncDesktop(ctx, cfg, st)
	case SourceAll:
		if err := slackapi.New(tokens).Sync(ctx, st, slackapi.SyncOptions{
			WorkspaceID: opts.WorkspaceID,
			Channels:    opts.Channels,
			Since:       opts.Since,
			Full:        opts.Full,
			Concurrency: opts.Concurrency,
		}); err != nil {
			return summary, err
		}
		return syncDesktop(ctx, cfg, st)
	default:
		return summary, errors.New("unsupported source")
	}
}

func syncDesktop(ctx context.Context, cfg config.Config, st *store.Store) (Summary, error) {
	if !cfg.Slack.Desktop.Enabled {
		return Summary{Desktop: slackdesktop.Source{Path: cfg.Slack.Desktop.Path, Available: false}}, nil
	}
	source, err := slackdesktop.Ingest(ctx, st, cfg.Slack.Desktop.Path)
	if err != nil {
		return Summary{}, err
	}
	return Summary{Desktop: source}, nil
}
