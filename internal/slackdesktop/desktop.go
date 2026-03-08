package slackdesktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Source struct {
	Path      string
	Available bool
	Summary   RootStateSummary
}

type RootStateSummary struct {
	AppTeamsKeys   []string `json:"app_teams_keys"`
	WorkspaceCount int      `json:"workspace_count"`
	TeamsCount     int      `json:"teams_count"`
}

type rootState struct {
	AppTeams   map[string]json.RawMessage `json:"appTeams"`
	Workspaces map[string]json.RawMessage `json:"workspaces"`
	Teams      map[string]json.RawMessage `json:"teams"`
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
	summary, err := LoadRootState(filepath.Join(path, "storage", "root-state.json"))
	if err != nil && !os.IsNotExist(err) {
		return Source{}, err
	}
	source.Summary = summary
	return source, nil
}

func LoadRootState(path string) (RootStateSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RootStateSummary{}, err
	}

	var state rootState
	if err := json.Unmarshal(data, &state); err != nil {
		return RootStateSummary{}, err
	}

	keys := make([]string, 0, len(state.AppTeams))
	for key := range state.AppTeams {
		keys = append(keys, key)
	}

	return RootStateSummary{
		AppTeamsKeys:   keys,
		WorkspaceCount: len(state.Workspaces),
		TeamsCount:     len(state.Teams),
	}, nil
}
