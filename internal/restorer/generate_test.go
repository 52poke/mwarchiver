package restorer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareNewImageStack(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dump := writeTestFile(t, filepath.Join(root, "dump.xml"), "<mediawiki/>", 0o600)
	o := Options{DumpPath: dump, StateDir: filepath.Join(root, "state"), Target: TargetNew}
	if err := o.Normalize(); err != nil {
		t.Fatal(err)
	}
	layout, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	settings := readTestFile(t, layout.SettingsPath)
	compose := readTestFile(t, layout.ComposePath)
	assertContains(t, settings, "$wgMainCacheType = CACHE_NONE;")
	assertNotContains(t, settings, "wfLoadExtension( 'OAuth' )")
	assertNotContains(t, settings, "wfLoadExtension( 'EventBus' )")
	assertContains(t, compose, "  mariadb:")
	assertContains(t, compose, "  mediawiki:")
	assertNotContains(t, compose, "  kafka:")
	assertContains(t, compose, "mariadb-root-password:")
	validateCompose(t, layout)
	validatePHP(t, layout.SettingsPath)
}

func TestPrepareCheckoutWithIntegrations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dump := writeTestFile(t, filepath.Join(root, "dump.xml.gz"), "dump", 0o600)
	checkout := fakeCheckout(t, root)
	o := Options{
		DumpPath:         dump,
		StateDir:         filepath.Join(root, "state"),
		Target:           TargetNew,
		MediaWikiDir:     checkout,
		Elasticsearch:    true,
		EventBusURL:      "http://host.docker.internal:5001",
		OAuth:            true,
		OAuthCallbackURL: "http://localhost:3000/callback",
		HostUID:          "1234",
		HostGID:          "5678",
	}
	if err := o.Normalize(); err != nil {
		t.Fatal(err)
	}
	layout, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	settings := readTestFile(t, layout.SettingsPath)
	compose := readTestFile(t, layout.ComposePath)
	for _, expected := range []string{
		"CACHE_MEMCACHED", "CirrusSearch", "wfLoadExtension( 'OAuth' )",
		"wfLoadExtension( 'EventBus' )", "http://host.docker.internal:5001",
	} {
		assertContains(t, settings, expected)
	}
	for _, expected := range []string{
		"  wiki-52poke:", "  kafka:",
		`USER_ID: "1234"`, `GROUP_ID: "5678"`,
		"HOME: /home/www-data", "COMPOSER_HOME: /home/www-data/.composer",
	} {
		assertContains(t, compose, expected)
	}
	assertContains(t, compose, "depends_on: !override")
	if layout.BaseComposePath != filepath.Join(checkout, ".devcontainer", "docker-compose.yml") {
		t.Fatalf("unexpected devcontainer base: %s", layout.BaseComposePath)
	}
	assertNotContains(t, compose, "  maintenance:")
	validateCompose(t, layout)
	validatePHP(t, layout.SettingsPath)
}

func TestPrepareImageWithElasticsearch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dump := writeTestFile(t, filepath.Join(root, "dump.xml"), "dump", 0o600)
	o := Options{
		DumpPath:      dump,
		StateDir:      filepath.Join(root, "state"),
		Target:        TargetNew,
		Elasticsearch: true,
	}
	if err := o.Normalize(); err != nil {
		t.Fatal(err)
	}
	layout, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	settings := readTestFile(t, layout.SettingsPath)
	compose := readTestFile(t, layout.ComposePath)
	assertContains(t, settings, "wfLoadExtension( 'CirrusSearch' )")
	assertContains(t, compose, "  elasticsearch:")
	assertContains(t, compose, "  elasticsearch-data:")
	validateCompose(t, layout)
	validatePHP(t, layout.SettingsPath)
}

func TestPrepareExistingDatabase(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dump := writeTestFile(t, filepath.Join(root, "dump.xml"), "dump", 0o600)
	password := writeTestFile(t, filepath.Join(root, "db-password"), "existing-secret\n", 0o600)
	o := Options{
		DumpPath:       dump,
		StateDir:       filepath.Join(root, "state"),
		Target:         TargetExisting,
		DBHost:         "host.docker.internal",
		DBPasswordFile: password,
	}
	if err := o.Normalize(); err != nil {
		t.Fatal(err)
	}
	layout, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	compose := readTestFile(t, layout.ComposePath)
	assertNotContains(t, compose, "  mariadb:")
	assertNotContains(t, compose, "mariadb-root-password:")
	if got := readTestFile(t, filepath.Join(o.StateDir, "mariadb-password")); got != "existing-secret\n" {
		t.Fatalf("copied database password = %q", got)
	}
	validateCompose(t, layout)
	validatePHP(t, layout.SettingsPath)
}

func TestPrepareRefusesToReplaceCheckoutSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dump := writeTestFile(t, filepath.Join(root, "dump.xml"), "dump", 0o600)
	checkout := fakeCheckout(t, root)
	settingsPath := writeTestFile(t, filepath.Join(checkout, "LocalSettings.php"), "<?php // existing\n", 0o600)
	o := Options{
		DumpPath:     dump,
		StateDir:     filepath.Join(root, "state"),
		Target:       TargetNew,
		MediaWikiDir: checkout,
	}
	if err := o.Normalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(o); err == nil || !strings.Contains(err.Error(), "--force-settings") {
		t.Fatalf("expected force-settings error, got %v", err)
	}
	if got := readTestFile(t, settingsPath); got != "<?php // existing\n" {
		t.Fatalf("existing settings changed: %q", got)
	}
	o.ForceSettings = true
	layout, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, layout.SettingsBackup); got != "<?php // existing\n" {
		t.Fatalf("settings backup = %q", got)
	}
}

func TestCheckoutRemovesLegacyKafkaDependency(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is unavailable")
	}
	root := t.TempDir()
	dump := writeTestFile(t, filepath.Join(root, "dump.xml"), "dump", 0o600)
	o := Options{
		DumpPath:     dump,
		StateDir:     filepath.Join(root, "state"),
		Target:       TargetNew,
		MediaWikiDir: fakeCheckout(t, root),
	}
	if err := o.Normalize(); err != nil {
		t.Fatal(err)
	}
	layout, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(
		"docker", "compose",
		"--file", layout.BaseComposePath,
		"--file", layout.ComposePath,
		"config", "--format", "json",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("render combined Compose configuration: %v\n%s", err, output)
	}
	var config struct {
		Services map[string]struct {
			DependsOn map[string]any `json:"depends_on"`
		} `json:"services"`
	}
	if err := json.Unmarshal(output, &config); err != nil {
		t.Fatalf("decode combined Compose configuration: %v", err)
	}
	if _, exists := config.Services["wiki-52poke"].DependsOn["kafka"]; exists {
		t.Fatal("legacy devcontainer Kafka dependency was not removed")
	}
}

func fakeCheckout(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "mediawiki")
	for _, path := range []string{
		"maintenance/run.php",
		".devcontainer/Dockerfile",
		"docker/nginx.conf",
		"docker/php.ini",
		"docker/supervisord.conf",
	} {
		writeTestFile(t, filepath.Join(dir, path), "placeholder", 0o600)
	}
	writeTestFile(t, filepath.Join(dir, ".devcontainer", "docker-compose.yml"), `services:
  wiki-52poke:
    build:
      context: .
      args:
        USER_ID: "1000"
        GROUP_ID: "1000"
    depends_on:
      - elasticsearch
      - kafka
      - memcached
  elasticsearch:
    image: ghcr.io/52poke/52w-elasticsearch:latest
  memcached:
    image: memcached:1.6
  kafka:
    image: bitnamilegacy/kafka:3.8.0
`, 0o600)
	return dir
}

func writeTestFile(t *testing.T, path, contents string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertContains(t *testing.T, value, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected output to contain %q\n%s", expected, value)
	}
}

func assertNotContains(t *testing.T, value, unexpected string) {
	t.Helper()
	if strings.Contains(value, unexpected) {
		t.Fatalf("expected output not to contain %q\n%s", unexpected, value)
	}
}

func validateCompose(t *testing.T, layout Layout) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is unavailable")
	}
	args := []string{"compose"}
	if layout.BaseComposePath != "" {
		args = append(args, "--file", layout.BaseComposePath)
	}
	args = append(args, "--file", layout.ComposePath, "config", "--quiet")
	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("invalid Compose configuration: %v\n%s", err, output)
	}
}

func validatePHP(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php is unavailable")
	}
	output, err := exec.Command("php", "-l", path).CombinedOutput()
	if err != nil {
		t.Fatalf("invalid LocalSettings.php: %v\n%s", err, output)
	}
}
