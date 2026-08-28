package restorer

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceComposeArgsUseWWWData(t *testing.T) {
	t.Parallel()

	checkout := Runner{Options: Options{MediaWikiDir: "/checkout"}}
	wantCheckout := []string{"exec", "-T", "--user", "www-data", "wiki-52poke", "php", "maintenance/run.php"}
	if got := checkout.maintenanceComposeArgs("php", "maintenance/run.php"); !slices.Equal(got, wantCheckout) {
		t.Fatalf("checkout maintenance arguments = %#v, want %#v", got, wantCheckout)
	}

	image := Runner{}
	wantImage := []string{"run", "--rm", "--no-deps", "--user", "www-data", "maintenance", "php", "maintenance/run.php"}
	if got := image.maintenanceComposeArgs("php", "maintenance/run.php"); !slices.Equal(got, wantImage) {
		t.Fatalf("image maintenance arguments = %#v, want %#v", got, wantImage)
	}
}

func TestFormatImportProgressUsesResumePosition(t *testing.T) {
	t.Parallel()

	got, ok := formatImportProgress("100000 (9000.00 pages/sec 9000.00 revs/sec)", 275445, 99000, 10*time.Second)
	if !ok {
		t.Fatal("import report was not recognized")
	}
	for _, expected := range []string{"100000/275445", "36.3%", "ETA 29m0s"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("progress %q does not contain %q", got, expected)
		}
	}
}

func TestInstallIgnoresMountedLocalSettings(t *testing.T) {
	t.Parallel()

	runner := Runner{Options: Options{
		DBName:    "52poke_wiki",
		DBUser:    "mediawiki",
		Port:      8227,
		AdminUser: "Admin",
	}}
	args := runner.mediaWikiInstallArgs()
	if len(args) < 2 || args[0] != "env" || args[1] != "MW_CONFIG_FILE=/tmp/mwarchiver-no-local-settings.php" {
		t.Fatalf("installer does not override LocalSettings.php: %#v", args)
	}
	if !slices.Contains(args, "--confpath=/tmp") {
		t.Fatalf("installer does not isolate generated settings: %#v", args)
	}
}

func TestImportDumpArgsDefaultToDeferredUpdates(t *testing.T) {
	t.Parallel()

	runner := Runner{Options: Options{DumpPath: "/downloads/snapshot.xml.gz"}}
	args := runner.importDumpArgs()
	for _, expected := range []string{
		"MWARCHIVER_BULK_MAINTENANCE=1",
		"--no-updates",
		"--report=1000",
		"/restore/input/snapshot.xml.gz",
	} {
		if !slices.Contains(args, expected) {
			t.Fatalf("import arguments do not contain %q: %#v", expected, args)
		}
	}
}

func TestImportDumpArgsCanUpdateLinksAndResume(t *testing.T) {
	t.Parallel()

	runner := Runner{Options: Options{
		DumpPath:                "/downloads/snapshot.xml.gz",
		UpdateLinksDuringImport: true,
		ImportSkipTo:            97001,
	}}
	args := runner.importDumpArgs()
	if slices.Contains(args, "--no-updates") {
		t.Fatalf("inline update arguments unexpectedly disable updates: %#v", args)
	}
	if !slices.Contains(args, "--skip-to=97001") {
		t.Fatalf("resume position missing from import arguments: %#v", args)
	}
}

func TestOAuthClientArgsIncludeRequiredCallbackMode(t *testing.T) {
	t.Parallel()

	runner := Runner{Options: Options{
		AdminUser:        "Admin",
		OAuthName:        "Klinklang",
		OAuthDescription: "Local client",
		OAuthVersion:     "1.0",
		OAuthCallbackURL: "https://example.test/oauth/callback",
		OAuthGrants:      []string{"basic"},
	}}
	args := runner.oauthClientArgs()
	if !slices.Contains(args, "--callbackIsPrefix") {
		t.Fatalf("required OAuth callback mode missing: %#v", args)
	}
}
