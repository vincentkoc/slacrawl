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
	"sync"
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

type OutputFormat string

const (
	FormatText OutputFormat = "text"
	FormatJSON OutputFormat = "json"
	FormatLog  OutputFormat = "log"
)

func New() *App {
	return &App{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	global := flag.NewFlagSet("slacrawl", flag.ContinueOnError)
	global.SetOutput(a.Stderr)
	global.Usage = func() {}
	configPath := global.String("config", "", "config path")
	format := global.String("format", string(FormatText), "output format: text|json|log")
	jsonOut := global.Bool("json", false, "json output")
	noColor := global.Bool("no-color", false, "disable ANSI color in text output")
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.setColorEnabled(FormatText, *noColor)
			a.printHelp()
			return nil
		}
		return err
	}

	rest := global.Args()
	if len(rest) == 0 {
		a.setColorEnabled(FormatText, *noColor)
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

	outputFormat, err := resolveOutputFormat(*format, *jsonOut)
	if err != nil {
		return err
	}
	a.setColorEnabled(outputFormat, *noColor)

	switch rest[0] {
	case "init":
		return a.runInit(*configPath, rest[1:], outputFormat)
	case "doctor":
		return a.runDoctor(ctx, *configPath, outputFormat)
	case "status":
		return a.runStatus(ctx, *configPath, outputFormat)
	case "sync":
		return a.runSync(ctx, *configPath, rest[1:], outputFormat)
	case "search":
		return a.runSearch(ctx, *configPath, rest[1:], outputFormat)
	case "messages":
		return a.runMessages(ctx, *configPath, rest[1:], outputFormat)
	case "mentions":
		return a.runMentions(ctx, *configPath, rest[1:], outputFormat)
	case "sql":
		return a.runSQL(ctx, *configPath, rest[1:], outputFormat)
	case "users":
		return a.runUsers(ctx, *configPath, rest[1:], outputFormat)
	case "channels":
		return a.runChannels(ctx, *configPath, rest[1:], outputFormat)
	case "completion":
		return a.runCompletion(rest[1:])
	case "tail":
		return a.runTail(ctx, *configPath, rest[1:])
	case "watch":
		return a.runWatch(ctx, *configPath, rest[1:], outputFormat)
	default:
		return fmt.Errorf("unknown command: %s", rest[0])
	}
}

func (a *App) setColorEnabled(format OutputFormat, noColor bool) {
	ansiEnabled = format == FormatText && !noColor && colorAllowedByEnv() && writerIsTTY(a.Stdout)
}

func colorAllowedByEnv() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return true
}

func writerIsTTY(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func resolveOutputFormat(value string, jsonOut bool) (OutputFormat, error) {
	if jsonOut {
		return FormatJSON, nil
	}
	switch OutputFormat(strings.ToLower(strings.TrimSpace(value))) {
	case "", FormatText:
		return FormatText, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatLog:
		return FormatLog, nil
	default:
		return "", fmt.Errorf("unsupported format %q: use text, json, or log", value)
	}
}

func (a *App) runInit(configPath string, args []string, format OutputFormat) error {
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
	result := map[string]any{
		"config_path": configPath,
		"db_path":     cfg.DBPath,
	}
	return a.writeOutput("Init", result, format, true)
}

func (a *App) runDoctor(ctx context.Context, configPath string, format OutputFormat) error {
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
	workspaceAPI, err := a.workspaceDoctorReports(ctx, cfg)
	if err != nil {
		return err
	}
	desktop := slackdesktop.Source{Path: cfg.Slack.Desktop.Path, Available: false}
	if cfg.Slack.Desktop.Enabled {
		desktop, err = slackdesktop.Inspect(cfg.Slack.Desktop.Path)
		if err != nil {
			return err
		}
	}
	threadCoverage := diag.ThreadCoverage
	if len(workspaceAPI) > 0 {
		threadCoverage = aggregateThreadCoverage(workspaceAPI)
	}
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
	channelSkips, err := st.ListSyncState(ctx, "api-bot", "channel_skip", 20)
	if err != nil {
		return err
	}
	tailState, err := st.ListSyncState(ctx, "tail", "", 20)
	if err != nil {
		return err
	}

	report := map[string]any{
		"config_path":   configPath,
		"database_path": cfg.DBPath,
		"tokens": map[string]any{
			"bot_env":      cfg.Slack.Bot.TokenEnv,
			"app_env":      cfg.Slack.App.TokenEnv,
			"user_env":     cfg.Slack.User.TokenEnv,
			"bot_enabled":  cfg.Slack.Bot.Enabled,
			"app_enabled":  cfg.Slack.App.Enabled,
			"user_enabled": cfg.Slack.User.Enabled,
			"bot_set":      cfg.ResolveTokens().Bot != "",
			"app_set":      cfg.ResolveTokens().App != "",
			"user_set":     cfg.ResolveTokens().User != "",
		},
		"slack_api":         diag,
		"workspace_api":     workspaceAPI,
		"desktop_source":    desktop,
		"api_channel_skips": channelSkips,
		"tail_state":        tailState,
		"status":            status,
		"fts_available":     true,
	}
	return a.writeOutput("Doctor", report, format, true)
}

func (a *App) runStatus(ctx context.Context, configPath string, format OutputFormat) error {
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
	return a.writeOutput("Status", status, format, true)
}

func (a *App) runSync(ctx context.Context, configPath string, args []string, format OutputFormat) error {
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
	concurrency := fs.Int("concurrency", cfg.Sync.Concurrency, "worker count")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	runOptions := syncer.Options{
		Source:      syncer.Source(*source),
		WorkspaceID: coalesce(*workspaceID, cfg.WorkspaceID),
		Channels:    csv(*channels),
		Since:       *since,
		Full:        *full,
		Concurrency: *concurrency,
	}
	summary, err := a.runSyncTargets(ctx, cfg, st, runOptions)
	if err != nil {
		return err
	}
	status, err := st.Status(ctx)
	if err != nil {
		return err
	}
	result := map[string]any{
		"status":  status,
		"summary": summary,
	}
	return a.writeOutput("Sync", result, format, true)
}

func (a *App) runSearch(ctx context.Context, configPath string, args []string, format OutputFormat) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	workspaceID := fs.String("workspace", "", "workspace id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("search query required")
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Search(ctx, coalesce(*workspaceID, cfg.WorkspaceID), strings.Join(fs.Args(), " "), 50)
	if err != nil {
		return err
	}
	return a.writeOutput("Search", results, format, false)
}

func (a *App) runMessages(ctx context.Context, configPath string, args []string, format OutputFormat) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("messages", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	workspaceID := fs.String("workspace", "", "workspace id")
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
	results, err := st.Messages(ctx, coalesce(*workspaceID, cfg.WorkspaceID), *channelID, *userID, store.RequireLimit(*limit))
	if err != nil {
		return err
	}
	return a.writeOutput("Messages", results, format, false)
}

func (a *App) runMentions(ctx context.Context, configPath string, args []string, format OutputFormat) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("mentions", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	workspaceID := fs.String("workspace", "", "workspace id")
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
	results, err := st.Mentions(ctx, coalesce(*workspaceID, cfg.WorkspaceID), *target, store.RequireLimit(*limit))
	if err != nil {
		return err
	}
	return a.writeOutput("Mentions", results, format, false)
}

func (a *App) runSQL(ctx context.Context, configPath string, args []string, format OutputFormat) error {
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
	return a.writeOutput("SQL", results, format, false)
}

func (a *App) runUsers(ctx context.Context, configPath string, args []string, format OutputFormat) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("users", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	workspaceID := fs.String("workspace", "", "workspace id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := ""
	if fs.NArg() > 0 {
		query = fs.Arg(0)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Users(ctx, coalesce(*workspaceID, cfg.WorkspaceID), query, 100)
	if err != nil {
		return err
	}
	return a.writeOutput("Users", results, format, false)
}

func (a *App) runChannels(ctx context.Context, configPath string, args []string, format OutputFormat) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("channels", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	workspaceID := fs.String("workspace", "", "workspace id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := ""
	if fs.NArg() > 0 {
		query = fs.Arg(0)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	results, err := st.Channels(ctx, coalesce(*workspaceID, cfg.WorkspaceID), query, 100)
	if err != nil {
		return err
	}
	return a.writeOutput("Channels", results, format, false)
}

func (a *App) runTail(ctx context.Context, configPath string, args []string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("tail", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	workspaceID := fs.String("workspace", "", "workspace id")
	repairEvery := fs.String("repair-every", cfg.Sync.RepairEvery, "repair interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	repairDuration, err := time.ParseDuration(*repairEvery)
	if err != nil {
		return err
	}
	targets := resolveWorkspaceTargets(cfg, *workspaceID)
	if len(targets) == 0 {
		targets = []string{coalesce(*workspaceID, cfg.WorkspaceID)}
	}
	if len(targets) == 1 {
		return slackapi.New(cfg.ResolveTokensForWorkspace(targets[0])).Tail(ctx, st, targets[0], repairDuration)
	}
	return a.runTailTargets(ctx, st, cfg, targets, repairDuration)
}

func (a *App) runWatch(ctx context.Context, configPath string, args []string, format OutputFormat) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	desktopEvery := fs.String("desktop-every", cfg.Sync.DesktopRefreshEvery, "desktop refresh interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !cfg.Slack.Desktop.Enabled {
		return errors.New("desktop sync is disabled in config")
	}
	interval, err := time.ParseDuration(*desktopEvery)
	if err != nil {
		return err
	}
	if interval <= 0 {
		return errors.New("desktop refresh interval must be greater than zero")
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	syncOnce := func() error {
		summary, err := syncer.Run(ctx, cfg, st, syncer.Options{Source: syncer.SourceDesktop})
		if err != nil {
			return err
		}
		status, err := st.Status(ctx)
		if err != nil {
			return err
		}
		return a.writeOutput("Watch", map[string]any{
			"status":  status,
			"summary": summary,
		}, format, true)
	}
	if err := syncOnce(); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := syncOnce(); err != nil {
				return err
			}
		}
	}
}

func (a *App) writeJSON(value any) error {
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
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

func resolveWorkspaceTargets(cfg config.Config, requested string) []string {
	if strings.TrimSpace(requested) != "" {
		return []string{strings.TrimSpace(requested)}
	}
	if ids := cfg.WorkspaceIDs(); len(ids) > 0 {
		return ids
	}
	if cfg.WorkspaceID != "" {
		return []string{cfg.WorkspaceID}
	}
	return nil
}

func (a *App) runSyncTargets(ctx context.Context, cfg config.Config, st *store.Store, opts syncer.Options) (syncer.Summary, error) {
	targets := resolveWorkspaceTargets(cfg, opts.WorkspaceID)
	if opts.Source == syncer.SourceDesktop {
		return syncer.Run(ctx, cfg, st, opts)
	}
	if len(targets) == 0 {
		return syncer.Run(ctx, cfg, st, opts)
	}

	var last syncer.Summary
	for _, workspaceID := range targets {
		runOpts := opts
		runOpts.WorkspaceID = workspaceID
		summary, err := syncer.RunWithTokens(ctx, cfg, st, runOpts, cfg.ResolveTokensForWorkspace(workspaceID))
		if err != nil {
			return syncer.Summary{}, err
		}
		last = summary
	}
	return last, nil
}

func (a *App) runTailTargets(ctx context.Context, st *store.Store, cfg config.Config, workspaceIDs []string, repairEvery time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(workspaceIDs))
	var wg sync.WaitGroup
	for _, workspaceID := range workspaceIDs {
		workspaceID := workspaceID
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := slackapi.New(cfg.ResolveTokensForWorkspace(workspaceID)).Tail(ctx, st, workspaceID, repairEvery)
			if err != nil && !errors.Is(err, context.Canceled) {
				errCh <- fmt.Errorf("tail %s: %w", workspaceID, err)
				cancel()
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errCh:
		return err
	case <-done:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) workspaceDoctorReports(ctx context.Context, cfg config.Config) ([]map[string]any, error) {
	workspaceIDs := cfg.WorkspaceIDs()
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	reports := make([]map[string]any, 0, len(workspaceIDs))
	for _, workspaceID := range workspaceIDs {
		tokens := cfg.ResolveTokensForWorkspace(workspaceID)
		diag, err := slackapi.New(tokens).Doctor(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("doctor %s: %w", workspaceID, err)
		}
		reports = append(reports, map[string]any{
			"workspace_id": workspaceID,
			"tokens": map[string]any{
				"bot_set":  tokens.Bot != "",
				"app_set":  tokens.App != "",
				"user_set": tokens.User != "",
			},
			"slack_api": diag,
		})
	}
	return reports, nil
}

func aggregateThreadCoverage(reports []map[string]any) string {
	if len(reports) == 0 {
		return "partial"
	}
	for _, report := range reports {
		slackAPI, ok := report["slack_api"].(slackapi.Diagnostics)
		if !ok || slackAPI.ThreadCoverage != "full" {
			return "partial"
		}
	}
	return "full"
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
