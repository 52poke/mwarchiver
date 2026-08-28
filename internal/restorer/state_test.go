package restorer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatePersistsConfiguredServices(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dump := writeTestFile(t, filepath.Join(root, "dump.xml"), "dump", 0o600)
	o := Options{
		DumpPath:      dump,
		StateDir:      filepath.Join(root, "state"),
		Target:        TargetNew,
		Elasticsearch: true,
		EventBusURL:   "http://localhost:5001",
	}
	if err := o.Normalize(); err != nil {
		t.Fatal(err)
	}
	layout, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(o.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.ComposePath != layout.ComposePath {
		t.Fatalf("compose path = %q", state.ComposePath)
	}
	want := "mariadb,elasticsearch,mediawiki,kafka"
	if got := strings.Join(state.Services, ","); got != want {
		t.Fatalf("services = %q, want %q", got, want)
	}
}

func TestLifecycleDownPreservesVolumes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cli := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := State{
		Version:      stateVersion,
		ContainerCLI: cli,
		ProjectName:  "restore-test",
		ComposePath:  filepath.Join(stateDir, "compose.yaml"),
		Services:     []string{"mariadb", "mediawiki", "elasticsearch"},
		Port:         8227,
	}
	if err := writeState(filepath.Join(stateDir, "state.json"), state); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunLifecycle(context.Background(), stateDir, LifecycleDown, &output, &output); err != nil {
		t.Fatal(err)
	}
	args := output.String()
	if !strings.Contains(args, " down") {
		t.Fatalf("down arguments = %q", args)
	}
	if !strings.Contains(args, "down --remove-orphans") {
		t.Fatalf("down does not remove configured orphan containers: %q", args)
	}
	if strings.Contains(args, "--volumes") || strings.Contains(args, " -v") {
		t.Fatalf("down unexpectedly deletes volumes: %q", args)
	}
}

func TestLifecycleUpStartsRecordedServices(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cli := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := State{
		Version:      stateVersion,
		ContainerCLI: cli,
		ProjectName:  "restore-test",
		ComposePath:  filepath.Join(stateDir, "compose.yaml"),
		Services:     []string{"mariadb", "mediawiki", "elasticsearch"},
		BindAddress:  "192.0.2.25",
		Port:         9123,
	}
	if err := writeState(filepath.Join(stateDir, "state.json"), state); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunLifecycle(context.Background(), stateDir, LifecycleUp, &output, &output); err != nil {
		t.Fatal(err)
	}
	args := output.String()
	if !strings.Contains(args, "up -d mariadb mediawiki elasticsearch") {
		t.Fatalf("up arguments = %q", args)
	}
	if !strings.Contains(args, "http://192.0.2.25:9123") {
		t.Fatalf("up output does not include wiki URL: %q", args)
	}
}

func TestRebuildLinksUsesCheckoutMaintenanceContainer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cli := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := State{
		Version:         stateVersion,
		ContainerCLI:    cli,
		ProjectName:     "restore-test",
		BaseComposePath: filepath.Join(root, "devcontainer-compose.yaml"),
		ComposePath:     filepath.Join(stateDir, "compose.yaml"),
		Services:        []string{"mariadb", "wiki-52poke"},
		Port:            8227,
	}
	if err := writeState(filepath.Join(stateDir, "state.json"), state); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RebuildLinks(context.Background(), stateDir, &output, &output); err != nil {
		t.Fatal(err)
	}
	args := output.String()
	if !strings.Contains(args, "exec -T --user www-data wiki-52poke env MWARCHIVER_BULK_MAINTENANCE=1 php maintenance/run.php refreshLinks") {
		t.Fatalf("refreshLinks arguments = %q", args)
	}
	if !strings.Contains(args, "initSiteStats --update") || !strings.Contains(args, "runJobs") {
		t.Fatalf("follow-up maintenance commands missing: %q", args)
	}
}

func TestRebuildSearchIndexIsSeparateLifecycleStage(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := State{
		Version:      stateVersion,
		ContainerCLI: cli,
		ProjectName:  "restore-test",
		ComposePath:  filepath.Join(stateDir, "compose.yaml"),
		Services:     []string{"mariadb", "mediawiki", "elasticsearch"},
		Port:         8227,
	}
	if err := writeState(filepath.Join(stateDir, "state.json"), state); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RebuildSearchIndex(context.Background(), stateDir, &output, &output); err != nil {
		t.Fatal(err)
	}
	args := output.String()
	for _, expected := range []string{
		"UpdateSearchIndexConfig.php --startOver",
		"ForceSearchIndex.php --skipLinks --indexOnSkip",
		"ForceSearchIndex.php --skipParse",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("search index commands do not contain %q: %q", expected, args)
		}
	}
}

func TestRebuildSearchIndexRequiresElasticsearch(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := State{
		Version:      stateVersion,
		ContainerCLI: cli,
		ProjectName:  "restore-test",
		ComposePath:  filepath.Join(stateDir, "compose.yaml"),
		Services:     []string{"mariadb", "mediawiki"},
		Port:         8227,
	}
	if err := writeState(filepath.Join(stateDir, "state.json"), state); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := RebuildSearchIndex(context.Background(), stateDir, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "Elasticsearch was not enabled") {
		t.Fatalf("expected Elasticsearch configuration error, got %v", err)
	}
}
