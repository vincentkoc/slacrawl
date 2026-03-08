package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/vincentkoc/slacrawl/internal/config"
	"github.com/vincentkoc/slacrawl/internal/slackapi"
	"github.com/vincentkoc/slacrawl/internal/slackdesktop"
	"github.com/vincentkoc/slacrawl/internal/store"
	"github.com/vincentkoc/slacrawl/internal/syncer"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

func New() *App {
	return &App{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	global := flag.NewFlagSet("slacrawl", flag.ContinueOnError)
	global.SetOutput(a.Stderr)
	configPath := global.String("config", "", "config path")
	jsonOut := global.Bool("json", false, "json output")
	if err := global.Parse(args); err != nil {
		return err
	}

	rest := global.Args()
	if len(rest) == 0 {
		a.printHelp()
		return nil
	}

	if *configPath == "" {
		path, err := config.DefaultConfigPath()
		if err != nil {
			return err
		}
		*configPath = path
	}

	switch rest[0] {
	case "init":
		return a.runInit(*configPath, rest[1:], *jsonOut)
	case "doctor":
		return a.runDoctor(ctx, *configPath, *jsonOut)
	case "status":
		return a.runStatus(ctx, *configPath, *jsonOut)
	case "sync":
		return a.runSync(ctx, *configPath, rest[1:], *jsonOut)
	case "search":
		return a.runSearch(ctx, *configPath, rest[1:], *jsonOut)
	case "messages":
		return a.runMessages(ctx, *configPath, rest[1:], *jsonOut)
	case "mentions":
		return a.runMentions(ctx, *configPath, rest[1:], *jsonOut)
	case "sql":
		return a.runSQL(ctx, *configPath, rest[1:], *jsonOut)
	case "users":
		return a.runUsers(ctx, *configPath, rest[1:], *jsonOut)
	case "channels":
		return a.runChannels(ctx, *configPath, rest[1:], *jsonOut)
	case "tail":
		return a.runTail(ctx, *configPath)
	default:
		return fmt.Errorf("unknown command: %s", rest[0])
	}
}

func (a *App) runInit(configPath string, args []string, jsonOut bool) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	workspaceID := fs.String("workspace", "", "workspace id")
	dbPath := fs.String("db", "", "database path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg := config.Default()
	if *workspaceID != "" {
		cfg.WorkspaceID = *workspaceID
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := cfg.Save(configPath); err != nil {
		return err
	}
	return a.write(jsonOut, map[string]any{
		"config_path": configPath,
		"db_path":     cfg.DBPath,
	})
}

func (a *App) runDoctor(ctx context.Context, configPath string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	diag, err := slackapi.New(cfg.ResolveTokens()).Doctor(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	desktop, err := slackdesktop.Discover(cfg.Slack.Desktop.Path)
	if err != nil {
		return err
	}
	threadCoverage := diag.ThreadCoverage
	if threadCoverage == "" {
		threadCoverage = "partial"
	}
	if err := st.SetSyncState(ctx, "doctor", "threads", "coverage", threadCoverage); err != nil {
		return err
	}
	status, err := st.Status(ctx)
	if err != nil {
		return err
	}

	report := map[string]any{
		"config_path":   configPath,
		"database_path": cfg.DBPath,
		"tokens": map[string]any{
			"bot_env":  cfg.Slack.Bot.TokenEnv,
			"app_env":  cfg.Slack.App.TokenEnv,
			"user_env": cfg.Slack.User.TokenEnv,
			"bot_set":  cfg.ResolveTokens().Bot != "",
			"app_set":  cfg.ResolveTokens().App != "",
			"user_set": cfg.ResolveTokens().User != "",
		},
		"slack_api":      diag,
		"desktop_source": desktop,
		"status":         status,
		"fts_available":  true,
	}
	return a.write(jsonOut, report)
}

func (a *App) runStatus(ctx context.Context, configPath string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	status, err := st.Status(ctx)
	if err != nil {
		return err
	}
	if jsonOut {
		return a.write(true, status)
	}
	_, err = fmt.Fprintln(a.Stdout, store.PrettyStatus(status))
	return err
}

func (a *App) runSync(ctx context.Context, configPath string, args []string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	source := fs.String("source", "api", "api|desktop|all")
	workspaceID := fs.String("workspace", "", "workspace id")
	channels := fs.String("channels", "", "comma separated channel ids")
	since := fs.String("since", "", "oldest slack ts or RFC3339 timestamp")
	full := fs.Bool("full", false, "full sync")
	_ = fs.Int("concurrency", cfg.Sync.Concurrency, "worker count")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	summary, err := syncer.Run(ctx, cfg, st, syncer.Options{
		Source:      syncer.Source(*source),
		WorkspaceID: coalesce(*workspaceID, cfg.WorkspaceID),
		Channels:    csv(*channels),
		Since:       *since,
		Full:        *full,
	})
	if err != nil {
		return err
	}
	status, err := st.Status(ctx)
	if err != nil {
		return err
	}
	return a.write(jsonOut, map[string]any{
		"status":  status,
		"summary": summary,
	})
}

func (a *App) runSearch(ctx context.Context, configPath string, args []string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("search query required")
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Search(ctx, strings.Join(args, " "), 50)
	if err != nil {
		return err
	}
	return a.write(jsonOut, results)
}

func (a *App) runMessages(ctx context.Context, configPath string, args []string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("messages", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	channelID := fs.String("channel", "", "channel id")
	userID := fs.String("author", "", "user id")
	limit := fs.Int("limit", 50, "row limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Messages(ctx, *channelID, *userID, store.RequireLimit(*limit))
	if err != nil {
		return err
	}
	return a.write(jsonOut, results)
}

func (a *App) runMentions(ctx context.Context, configPath string, args []string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("mentions", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	target := fs.String("target", "", "target id or label")
	limit := fs.Int("limit", 50, "row limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Mentions(ctx, *target, store.RequireLimit(*limit))
	if err != nil {
		return err
	}
	return a.write(jsonOut, results)
}

func (a *App) runSQL(ctx context.Context, configPath string, args []string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		query = strings.TrimSpace(string(data))
	}
	if query == "" {
		return errors.New("sql query required")
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.QueryReadOnly(ctx, query)
	if err != nil {
		return err
	}
	return a.write(jsonOut, results)
}

func (a *App) runUsers(ctx context.Context, configPath string, args []string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	query := ""
	if len(args) > 0 {
		query = args[0]
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Users(ctx, query, 100)
	if err != nil {
		return err
	}
	return a.write(jsonOut, results)
}

func (a *App) runChannels(ctx context.Context, configPath string, args []string, jsonOut bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	query := ""
	if len(args) > 0 {
		query = args[0]
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Channels(ctx, query, 100)
	if err != nil {
		return err
	}
	return a.write(jsonOut, results)
}

func (a *App) runTail(_ context.Context, configPath string) error {
	_, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	return errors.New("tail bootstrap is not implemented yet; use doctor to validate xapp/xoxb tokens and sync for backfill")
}

func (a *App) write(jsonOut bool, value any) error {
	if jsonOut {
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.Stdout, string(data))
	return err
}

func (a *App) printHelp() {
	_, _ = fmt.Fprintln(a.Stdout, "slacrawl commands: init doctor sync tail search messages mentions sql users channels status")
}

func loadConfig(path string) (config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func csv(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func coalesce(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func WithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 30*time.Second)
}
