package restorer

import (
	"bytes"
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

type Runner struct {
	Options Options
	Out     io.Writer
	Err     io.Writer
	layout  Layout
}

func (r *Runner) Run(ctx context.Context) error {
	if r.Out == nil {
		r.Out = os.Stdout
	}
	if r.Err == nil {
		r.Err = os.Stderr
	}
	if err := r.Options.Normalize(); err != nil {
		return err
	}
	if _, err := exec.LookPath(r.Options.ContainerCLI); err != nil {
		return fmt.Errorf("find container CLI %q: %w", r.Options.ContainerCLI, err)
	}
	var err error
	r.layout, err = Prepare(r.Options)
	if err != nil {
		return err
	}
	fmt.Fprintf(r.Out, "Prepared restore configuration in %s\n", r.Options.StateDir)
	if r.layout.SettingsBackup != "" {
		if _, err := os.Stat(r.layout.SettingsBackup); err == nil {
			fmt.Fprintf(r.Out, "Previous LocalSettings.php backup: %s\n", r.layout.SettingsBackup)
		}
	}

	if r.Options.Target == TargetNew {
		if err := r.startNewDatabase(ctx); err != nil {
			return err
		}
	}
	if r.Options.MediaWikiDir != "" {
		services := []string{"memcached"}
		if r.Options.Elasticsearch {
			services = append(services, "elasticsearch")
		}
		services = append(services, "wiki-52poke")
		if r.Options.EventBusURL != "" {
			services = append(services, "kafka")
		}
		if err := r.compose(ctx, append([]string{"up", "-d", "--build"}, services...)...); err != nil {
			return err
		}
		if err := r.updateCheckoutDependencies(ctx); err != nil {
			return err
		}
	} else {
		services := make([]string, 0, 2)
		if r.Options.Elasticsearch {
			services = append(services, "elasticsearch")
		}
		if r.Options.EventBusURL != "" {
			services = append(services, "kafka")
		}
		if len(services) > 0 {
			if err := r.compose(ctx, append([]string{"up", "-d"}, services...)...); err != nil {
				return err
			}
		}
	}
	if r.Options.Elasticsearch {
		if err := r.waitForService(ctx, "Elasticsearch", "elasticsearch", "curl", "-fsS", "http://localhost:9200"); err != nil {
			return err
		}
	}
	if r.Options.EventBusURL != "" {
		if err := r.waitForService(ctx, "Kafka", "kafka", "/opt/bitnami/kafka/bin/kafka-topics.sh", "--bootstrap-server", "localhost:9092", "--list"); err != nil {
			return err
		}
	}

	if r.Options.Target == TargetNew {
		hasSchema, err := r.databaseHasSchema(ctx)
		if err != nil {
			return err
		}
		if !hasSchema {
			fmt.Fprintln(r.Out, "Initializing MediaWiki database tables")
			if err := r.installMediaWiki(ctx); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(r.Out, "MediaWiki tables already exist; preserving them")
		}
	}

	if err := r.maintenance(ctx, "php", "maintenance/run.php", "update", "--quick"); err != nil {
		return fmt.Errorf("update MediaWiki schema: %w", err)
	}
	if err := r.createAdmin(ctx); err != nil {
		return err
	}

	fmt.Fprintf(r.Out, "Importing %s\n", r.Options.DumpPath)
	if err := r.maintenance(
		ctx,
		"php", "maintenance/run.php", "importDump",
		"--no-local-users", "--username-prefix=52poke",
		filepath.Join("/restore/input", filepath.Base(r.Options.DumpPath)),
	); err != nil {
		return fmt.Errorf("import XML dump: %w", err)
	}
	if err := r.maintenance(ctx, "php", "maintenance/run.php", "initSiteStats", "--update"); err != nil {
		return fmt.Errorf("update site statistics: %w", err)
	}
	if r.Options.Elasticsearch && !r.Options.SkipSearchIndex {
		if err := r.initializeSearch(ctx); err != nil {
			return err
		}
	}
	if r.Options.OAuth {
		if err := r.createOAuthClient(ctx); err != nil {
			return err
		}
	}
	if err := r.maintenance(ctx, "php", "maintenance/run.php", "runJobs"); err != nil {
		return fmt.Errorf("run MediaWiki jobs: %w", err)
	}

	if r.Options.MediaWikiDir == "" {
		if err := r.compose(ctx, "up", "-d", "mediawiki"); err != nil {
			return err
		}
	}

	fmt.Fprintf(r.Out, "\nLocal wiki: http://localhost:%d\n", r.Options.Port)
	fmt.Fprintf(r.Out, "Admin user: %s\n", r.Options.AdminUser)
	fmt.Fprintf(r.Out, "Admin password: %s\n", r.layout.AdminPassword)
	if r.Options.OAuth {
		fmt.Fprintf(r.Out, "OAuth client credentials: %s\n", r.layout.OAuthCredentials)
	}
	return nil
}

func (r *Runner) initializeSearch(ctx context.Context) error {
	fmt.Fprintln(r.Out, "Building the CirrusSearch index")
	if err := r.maintenance(
		ctx, "php", "maintenance/run.php",
		"extensions/CirrusSearch/maintenance/UpdateSearchIndexConfig.php", "--startOver",
	); err != nil {
		return fmt.Errorf("create CirrusSearch index: %w", err)
	}
	if err := r.maintenance(
		ctx, "php", "maintenance/run.php",
		"extensions/CirrusSearch/maintenance/ForceSearchIndex.php", "--skipLinks", "--indexOnSkip",
	); err != nil {
		return fmt.Errorf("build initial CirrusSearch index: %w", err)
	}
	if err := r.maintenance(
		ctx, "php", "maintenance/run.php",
		"extensions/CirrusSearch/maintenance/ForceSearchIndex.php", "--skipParse",
	); err != nil {
		return fmt.Errorf("finish CirrusSearch index: %w", err)
	}
	return nil
}

func (r *Runner) startNewDatabase(ctx context.Context) error {
	if err := r.compose(ctx, "up", "-d", "mariadb"); err != nil {
		return err
	}
	fmt.Fprintln(r.Out, "Waiting for MariaDB")
	var lastErr error
	for range 60 {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		lastErr = r.composeQuiet(checkCtx, "exec", "-T", "mariadb", "healthcheck.sh", "--connect", "--innodb_initialized")
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
	return fmt.Errorf("MariaDB did not become healthy: %w", lastErr)
}

func (r *Runner) waitForService(ctx context.Context, label, service string, command ...string) error {
	fmt.Fprintf(r.Out, "Waiting for %s\n", label)
	var lastErr error
	for range 60 {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		args := append([]string{"exec", "-T", service}, command...)
		lastErr = r.composeQuiet(checkCtx, args...)
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
	return fmt.Errorf("%s did not become ready: %w", label, lastErr)
}

func (r *Runner) databaseHasSchema(ctx context.Context) (bool, error) {
	const query = `export MYSQL_PWD="$(cat /run/secrets/mariadb-password)"
exec mariadb --batch --skip-column-names --user="$MARIADB_USER" "$MARIADB_DATABASE" --execute="SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'page'"`
	var output bytes.Buffer
	err := r.composeCapture(ctx, &output, "exec", "-T", "mariadb", "sh", "-euc", query)
	if err != nil {
		return false, fmt.Errorf("inspect MediaWiki database: %w", err)
	}
	return strings.TrimSpace(output.String()) == "1", nil
}

func (r *Runner) installMediaWiki(ctx context.Context) error {
	return r.maintenance(ctx, r.mediaWikiInstallArgs()...)
}

func (r *Runner) mediaWikiInstallArgs() []string {
	return []string{
		"env", "MW_CONFIG_FILE=/tmp/mwarchiver-no-local-settings.php",
		"php", "maintenance/run.php", "install",
		"--dbtype=mysql",
		"--dbserver=mariadb",
		"--dbname=" + r.Options.DBName,
		"--dbuser=" + r.Options.DBUser,
		"--dbpassfile=/run/secrets/mariadb-password",
		fmt.Sprintf("--server=http://localhost:%d", r.Options.Port),
		"--scriptpath=",
		"--lang=zh",
		"--passfile=/run/secrets/mediawiki-admin-password",
		"--confpath=/tmp",
		"神奇宝贝百科",
		r.Options.AdminUser,
	}
}

func (r *Runner) updateCheckoutDependencies(ctx context.Context) error {
	fmt.Fprintln(r.Out, "Updating checkout Composer dependencies")
	if err := r.maintenance(
		ctx,
		"composer", "update",
		"--no-dev",
		"--ignore-platform-req=ext-calendar",
		"--ignore-platform-req=ext-intl",
		"--with-all-dependencies",
	); err != nil {
		return fmt.Errorf("update checkout Composer dependencies: %w", err)
	}
	return nil
}

func (r *Runner) createAdmin(ctx context.Context) error {
	const script = `exec php maintenance/run.php createAndPromote --force --sysop --bureaucrat --interface-admin --reason="Created by mwarchiver restore" "$1" "$(cat /run/secrets/mediawiki-admin-password)"`
	if err := r.maintenance(ctx, "sh", "-euc", script, "mwarchiver-admin", r.Options.AdminUser); err != nil {
		return fmt.Errorf("create administrator: %w", err)
	}
	if r.Options.OAuth {
		if err := r.maintenance(
			ctx,
			"php", "maintenance/run.php", "resetUserEmail",
			"--no-reset-password", "--reason=Required for local OAuth client",
			r.Options.AdminUser, r.Options.AdminEmail,
		); err != nil {
			return fmt.Errorf("set administrator email: %w", err)
		}
	}
	return nil
}

func (r *Runner) createOAuthClient(ctx context.Context) error {
	if _, err := os.Stat(r.layout.OAuthCredentials); err == nil {
		fmt.Fprintf(r.Out, "OAuth client credentials already exist at %s; keeping the existing client\n", r.layout.OAuthCredentials)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect OAuth client credentials: %w", err)
	}
	args := []string{
		"php", "extensions/OAuth/maintenance/createOAuthConsumer.php",
		"--user=" + r.Options.AdminUser,
		"--name=" + r.Options.OAuthName,
		"--description=" + r.Options.OAuthDescription,
		"--version=" + r.Options.OAuthVersion,
		"--callbackUrl=" + r.Options.OAuthCallbackURL,
		"--approve", "--jsonOnSuccess",
	}
	for _, grant := range r.Options.OAuthGrants {
		args = append(args, "--grants="+grant)
	}
	var output bytes.Buffer
	if err := r.maintenanceCapture(ctx, &output, args...); err != nil {
		return fmt.Errorf("create OAuth client: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return fmt.Errorf("decode OAuth client result: %w", err)
	}
	formatted, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode OAuth client credentials: %w", err)
	}
	formatted = append(formatted, '\n')
	if err := writeAtomic(r.layout.OAuthCredentials, formatted, 0o600); err != nil {
		return fmt.Errorf("write OAuth client credentials: %w", err)
	}
	return nil
}

func (r *Runner) maintenance(ctx context.Context, args ...string) error {
	return r.compose(ctx, r.maintenanceComposeArgs(args...)...)
}

func (r *Runner) maintenanceCapture(ctx context.Context, output io.Writer, args ...string) error {
	return r.composeCapture(ctx, output, r.maintenanceComposeArgs(args...)...)
}

func (r *Runner) maintenanceComposeArgs(args ...string) []string {
	if r.Options.MediaWikiDir != "" {
		return append([]string{"exec", "-T", "--user", "www-data", "wiki-52poke"}, args...)
	}
	return append([]string{"run", "--rm", "--no-deps", "--user", "www-data", "maintenance"}, args...)
}

func (r *Runner) compose(ctx context.Context, args ...string) error {
	command := r.composeCommand(ctx, args...)
	command.Stdout = r.Out
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s compose %s: %w", r.Options.ContainerCLI, strings.Join(args, " "), err)
	}
	return nil
}

func (r *Runner) composeQuiet(ctx context.Context, args ...string) error {
	command := r.composeCommand(ctx, args...)
	return command.Run()
}

func (r *Runner) composeCapture(ctx context.Context, output io.Writer, args ...string) error {
	command := r.composeCommand(ctx, args...)
	command.Stdout = output
	command.Stderr = r.Err
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s compose %s: %w", r.Options.ContainerCLI, strings.Join(args, " "), err)
	}
	return nil
}

func (r *Runner) composeCommand(ctx context.Context, args ...string) *exec.Cmd {
	base := []string{"compose", "--project-name", r.Options.ProjectName}
	if r.layout.BaseComposePath != "" {
		base = append(base, "--file", r.layout.BaseComposePath)
	}
	base = append(base, "--file", r.layout.ComposePath)
	return exec.CommandContext(ctx, r.Options.ContainerCLI, append(base, args...)...)
}
