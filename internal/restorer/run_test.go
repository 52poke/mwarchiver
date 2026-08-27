package restorer

import (
	"slices"
	"testing"
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
