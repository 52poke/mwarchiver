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
	"time"
)

const stateVersion = 1

type State struct {
	Version         int      `json:"version"`
	ContainerCLI    string   `json:"container_cli"`
	ProjectName     string   `json:"project_name"`
	BaseComposePath string   `json:"base_compose_path,omitempty"`
	ComposePath     string   `json:"compose_path"`
	Services        []string `json:"services"`
	BindAddress     string   `json:"bind_address,omitempty"`
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
		BindAddress:     o.BindAddress,
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
	if state.BindAddress == "" {
		state.BindAddress = "127.0.0.1"
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
	args, err := state.composeArgs()
	if err != nil {
		return err
	}
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
		options := Options{BindAddress: state.BindAddress, Port: state.Port}
		fmt.Fprintf(out, "Local wiki: %s\n", options.WikiURL())
	}
	return nil
}

// RebuildLinks populates the derived tables intentionally skipped by the fast
// default import. It is a separate stage so the wiki's page and revision data
// becomes usable without waiting for every page to be parsed.
func RebuildLinks(ctx context.Context, stateDir string, out, errOut io.Writer) error {
	state, err := LoadState(stateDir)
	if err != nil {
		return err
	}
	base, err := state.composeArgs()
	if err != nil {
		return err
	}

	upArgs := append(append([]string{}, base...), "up", "-d")
	upArgs = append(upArgs, state.Services...)
	if err := runStateCommand(ctx, state, upArgs, out, errOut); err != nil {
		return err
	}

	fmt.Fprintln(out, "Rebuilding MediaWiki link, category, and related derived tables")
	commands := [][]string{
		{"env", "MWARCHIVER_BULK_MAINTENANCE=1", "php", "maintenance/run.php", "refreshLinks"},
		{"env", "MWARCHIVER_BULK_MAINTENANCE=1", "php", "maintenance/run.php", "initSiteStats", "--update"},
		{"env", "MWARCHIVER_BULK_MAINTENANCE=1", "php", "maintenance/run.php", "runJobs"},
	}
	for _, maintenance := range commands {
		args := append(append([]string{}, base...), state.maintenanceArgs(maintenance...)...)
		if err := runStateCommand(ctx, state, args, out, errOut); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Derived link and category data rebuild complete")
	if state.hasService("elasticsearch") {
		fmt.Fprintln(out, "Elasticsearch is configured; run `mwarchiver restore rebuild-search-index` to refresh its index.")
	}
	return nil
}

// RebuildSearchIndex creates and fills the CirrusSearch index as an explicit
// stage, after content and (preferably) derived link data have been restored.
func RebuildSearchIndex(ctx context.Context, stateDir string, out, errOut io.Writer) error {
	state, err := LoadState(stateDir)
	if err != nil {
		return err
	}
	if !state.hasService("elasticsearch") {
		return fmt.Errorf("Elasticsearch was not enabled for this prepared restore")
	}
	base, err := state.composeArgs()
	if err != nil {
		return err
	}

	upArgs := append(append([]string{}, base...), "up", "-d")
	upArgs = append(upArgs, state.Services...)
	if err := runStateCommand(ctx, state, upArgs, out, errOut); err != nil {
		return err
	}
	if err := waitForStateElasticsearch(ctx, state, base, out); err != nil {
		return err
	}

	fmt.Fprintln(out, "Creating and populating the CirrusSearch index")
	commands := [][]string{
		{"php", "maintenance/run.php", "extensions/CirrusSearch/maintenance/UpdateSearchIndexConfig.php", "--startOver"},
		{"php", "maintenance/run.php", "extensions/CirrusSearch/maintenance/ForceSearchIndex.php", "--skipLinks", "--indexOnSkip"},
		{"php", "maintenance/run.php", "extensions/CirrusSearch/maintenance/ForceSearchIndex.php", "--skipParse"},
	}
	for _, maintenance := range commands {
		args := append(append([]string{}, base...), state.maintenanceArgs(maintenance...)...)
		if err := runStateCommand(ctx, state, args, out, errOut); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "CirrusSearch index rebuild complete")
	return nil
}

func waitForStateElasticsearch(ctx context.Context, state State, base []string, out io.Writer) error {
	fmt.Fprintln(out, "Waiting for Elasticsearch")
	checkArgs := append(append([]string{}, base...),
		"exec", "-T", "elasticsearch", "curl", "-fsS", "http://localhost:9200",
	)
	var lastErr error
	for range 60 {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		command := exec.CommandContext(checkCtx, state.ContainerCLI, checkArgs...)
		lastErr = command.Run()
		cancel()
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("Elasticsearch did not become ready: %w", lastErr)
}

func (state State) hasService(name string) bool {
	for _, service := range state.Services {
		if service == name {
			return true
		}
	}
	return false
}

func (state State) composeArgs() ([]string, error) {
	if _, err := exec.LookPath(state.ContainerCLI); err != nil {
		return nil, fmt.Errorf("find container CLI %q: %w", state.ContainerCLI, err)
	}
	args := []string{"compose", "--project-name", state.ProjectName}
	if state.BaseComposePath != "" {
		args = append(args, "--file", state.BaseComposePath)
	}
	return append(args, "--file", state.ComposePath), nil
}

func (state State) maintenanceArgs(args ...string) []string {
	if state.BaseComposePath != "" {
		return append([]string{"exec", "-T", "--user", "www-data", "wiki-52poke"}, args...)
	}
	return append([]string{"run", "--rm", "--no-deps", "--user", "www-data", "maintenance"}, args...)
}

func runStateCommand(ctx context.Context, state State, args []string, out, errOut io.Writer) error {
	command := exec.CommandContext(ctx, state.ContainerCLI, args...)
	command.Stdout = out
	command.Stderr = errOut
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", state.ContainerCLI, strings.Join(args, " "), err)
	}
	return nil
}
