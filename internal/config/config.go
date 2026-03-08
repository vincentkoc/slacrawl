package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	defaultDirName     = ".slacrawl"
	defaultDesktopPath = "~/Library/Containers/com.tinyspeck.slackmacgap/Data/Library/Application Support/Slack"
)

type Config struct {
	Version     int          `toml:"version"`
	WorkspaceID string       `toml:"workspace_id"`
	DBPath      string       `toml:"db_path"`
	CacheDir    string       `toml:"cache_dir"`
	LogDir      string       `toml:"log_dir"`
	Slack       SlackConfig  `toml:"slack"`
	Sync        SyncConfig   `toml:"sync"`
	Search      SearchConfig `toml:"search"`
}

type SlackConfig struct {
	Bot     TokenConfig   `toml:"bot"`
	App     TokenConfig   `toml:"app"`
	User    TokenConfig   `toml:"user"`
	Desktop DesktopConfig `toml:"desktop"`
}

type TokenConfig struct {
	TokenEnv string `toml:"token_env"`
}

type DesktopConfig struct {
	Enabled bool   `toml:"enabled"`
	Path    string `toml:"path"`
}

type SyncConfig struct {
	Concurrency int    `toml:"concurrency"`
	RepairEvery string `toml:"repair_every"`
	FullHistory bool   `toml:"full_history"`
}

type SearchConfig struct {
	DefaultMode string `toml:"default_mode"`
}

type Tokens struct {
	Bot  string
	App  string
	User string
}

func Default() Config {
	base := "~/" + defaultDirName
	return Config{
		Version:  1,
		DBPath:   filepath.ToSlash(filepath.Join(base, "slacrawl.db")),
		CacheDir: filepath.ToSlash(filepath.Join(base, "cache")),
		LogDir:   filepath.ToSlash(filepath.Join(base, "logs")),
		Slack: SlackConfig{
			Bot:  TokenConfig{TokenEnv: "SLACK_BOT_TOKEN"},
			App:  TokenConfig{TokenEnv: "SLACK_APP_TOKEN"},
			User: TokenConfig{TokenEnv: "SLACK_USER_TOKEN"},
			Desktop: DesktopConfig{
				Enabled: true,
				Path:    defaultDesktopPath,
			},
		},
		Sync: SyncConfig{
			Concurrency: 4,
			RepairEvery: "30m",
			FullHistory: true,
		},
		Search: SearchConfig{
			DefaultMode: "fts",
		},
	}
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultDirName, "config.toml"), nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Normalize(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Save(path string) error {
	if err := c.Normalize(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *Config) Normalize() error {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Sync.Concurrency <= 0 {
		c.Sync.Concurrency = 4
	}
	if c.Search.DefaultMode == "" {
		c.Search.DefaultMode = "fts"
	}

	paths := []*string{&c.DBPath, &c.CacheDir, &c.LogDir, &c.Slack.Desktop.Path}
	for _, candidate := range paths {
		expanded, err := ExpandPath(*candidate)
		if err != nil {
			return err
		}
		*candidate = expanded
	}
	return nil
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Clean(path), nil
}

func (c Config) ResolveTokens() Tokens {
	return Tokens{
		Bot:  os.Getenv(c.Slack.Bot.TokenEnv),
		App:  os.Getenv(c.Slack.App.TokenEnv),
		User: os.Getenv(c.Slack.User.TokenEnv),
	}
}

func ValidateTokenShape(value string, prefix string) error {
	if value == "" {
		return errors.New("token missing")
	}
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("token must start with %s", prefix)
	}
	return nil
}

func Redact(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "…" + value[len(value)-4:]
}
