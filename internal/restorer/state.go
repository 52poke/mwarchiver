package restorer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const stateVersion = 1

type State struct {
	Version         int      `json:"version"`
	ContainerCLI    string   `json:"container_cli"`
	ProjectName     string   `json:"project_name"`
	BaseComposePath string   `json:"base_compose_path,omitempty"`
	ComposePath     string   `json:"compose_path"`
	Services        []string `json:"services"`
	Port            int      `json:"port"`
}

func stateFor(o Options, layout Layout) State {
	services := make([]string, 0, 6)
	if o.Target == TargetNew {
		services = append(services, "mariadb")
	}
	if o.MediaWikiDir != "" {
		services = append(services, "memcached")
	}
	if o.Elasticsearch {
		services = append(services, "elasticsearch")
	}
	if o.MediaWikiDir != "" {
		services = append(services, "wiki-52poke")
	} else {
		services = append(services, "mediawiki")
	}
	if o.EventBusURL != "" {
		services = append(services, "kafka")
	}
	return State{
		Version:         stateVersion,
		ContainerCLI:    o.ContainerCLI,
		ProjectName:     o.ProjectName,
		BaseComposePath: layout.BaseComposePath,
		ComposePath:     layout.ComposePath,
		Services:        services,
		Port:            o.Port,
	}
}

func writeState(path string, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode restore state: %w", err)
	}
	data = append(data, '\n')
	if err := writeAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write restore state: %w", err)
	}
	return nil
}

func LoadState(stateDir string) (State, error) {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return State{}, fmt.Errorf("resolve state directory: %w", err)
	}
	path := filepath.Join(abs, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, fmt.Errorf("no prepared restore found in %s", abs)
		}
		return State{}, fmt.Errorf("read restore state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode restore state: %w", err)
	}
	if state.Version != stateVersion {
		return State{}, fmt.Errorf("unsupported restore state version %d", state.Version)
	}
	if state.ContainerCLI == "" || state.ProjectName == "" || state.ComposePath == "" || len(state.Services) == 0 {
		return State{}, fmt.Errorf("restore state is incomplete: %s", path)
	}
	return state, nil
}

type LifecycleAction string

const (
	LifecycleUp     LifecycleAction = "up"
	LifecycleDown   LifecycleAction = "down"
	LifecycleStatus LifecycleAction = "status"
)

func RunLifecycle(ctx context.Context, stateDir string, action LifecycleAction, out, errOut io.Writer) error {
	state, err := LoadState(stateDir)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(state.ContainerCLI); err != nil {
		return fmt.Errorf("find container CLI %q: %w", state.ContainerCLI, err)
	}
	args := []string{"compose", "--project-name", state.ProjectName}
	if state.BaseComposePath != "" {
		args = append(args, "--file", state.BaseComposePath)
	}
	args = append(args, "--file", state.ComposePath)
	switch action {
	case LifecycleUp:
		args = append(args, "up", "-d")
		args = append(args, state.Services...)
	case LifecycleDown:
		// Intentionally omit --volumes: MariaDB, Elasticsearch, and Kafka data persist.
		args = append(args, "down", "--remove-orphans")
	case LifecycleStatus:
		args = append(args, "ps")
	default:
		return fmt.Errorf("unknown lifecycle action %q", action)
	}
	command := exec.CommandContext(ctx, state.ContainerCLI, args...)
	command.Stdout = out
	command.Stderr = errOut
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", state.ContainerCLI, strings.Join(args, " "), err)
	}
	if action == LifecycleUp {
		fmt.Fprintf(out, "Local wiki: http://localhost:%d\n", state.Port)
	}
	return nil
}
