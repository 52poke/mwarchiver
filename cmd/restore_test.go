package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreWizardPromptsForBindAddress(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dumpPath := filepath.Join(root, "dump.xml")
	if err := os.WriteFile(dumpPath, []byte("<mediawiki/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	command := newRestoreCommand()
	command.SetArgs([]string{dumpPath, "--prepare-only", "--state-dir", stateDir})
	command.SetIn(strings.NewReader("\n\n192.0.2.25\n\n\n\n\n"))
	var output bytes.Buffer
	var errors bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&errors)

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errors.String(), "Host IP address on which to expose the wiki") {
		t.Fatalf("wizard did not prompt for a bind address:\n%s", errors.String())
	}
	compose, err := os.ReadFile(filepath.Join(stateDir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), `"192.0.2.25:8227:80"`) {
		t.Fatalf("wizard bind address not written to Compose:\n%s", compose)
	}
}
