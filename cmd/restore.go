package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/52poke/mwarchiver/internal/restorer"
	"github.com/spf13/cobra"
)

type restoreFlags struct {
	target             string
	mediaWikiDir       string
	image              string
	stateDir           string
	containerCLI       string
	projectName        string
	port               int
	dbHost             string
	dbPort             int
	dbName             string
	dbUser             string
	dbPasswordFile     string
	adminUser          string
	adminEmail         string
	elasticsearch      bool
	elasticsearchImage string
	eventBusURL        string
	oauth              bool
	oauthName          string
	oauthDescription   string
	oauthVersion       string
	oauthCallback      string
	oauthGrants        []string
	forceSettings      bool
	nonInteractive     bool
	prepareOnly        bool
	skipSearchIndex    bool
}

func newRestoreCommand() *cobra.Command {
	var flags restoreFlags
	command := &cobra.Command{
		Use:   "restore DUMP",
		Short: "Restore a MediaWiki XML snapshot into a local wiki",
		Long: `Restore a decrypted MediaWiki XML dump into either a new local database
or an existing MediaWiki database. A 52poke/mediawiki checkout uses its
devcontainer build and Memcached; otherwise the published
ghcr.io/52poke/mediawiki image is used. Elasticsearch is an optional,
persistent service managed by this restore workflow.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := restorer.Options{
				DumpPath:           args[0],
				StateDir:           flags.stateDir,
				Target:             restorer.Target(flags.target),
				MediaWikiDir:       flags.mediaWikiDir,
				Image:              flags.image,
				ContainerCLI:       flags.containerCLI,
				ProjectName:        flags.projectName,
				Port:               flags.port,
				DBHost:             flags.dbHost,
				DBPort:             flags.dbPort,
				DBName:             flags.dbName,
				DBUser:             flags.dbUser,
				DBPasswordFile:     flags.dbPasswordFile,
				AdminUser:          flags.adminUser,
				AdminEmail:         flags.adminEmail,
				Elasticsearch:      flags.elasticsearch,
				ElasticsearchImage: flags.elasticsearchImage,
				EventBusURL:        flags.eventBusURL,
				OAuth:              flags.oauth,
				OAuthName:          flags.oauthName,
				OAuthDescription:   flags.oauthDescription,
				OAuthVersion:       flags.oauthVersion,
				OAuthCallbackURL:   flags.oauthCallback,
				OAuthGrants:        flags.oauthGrants,
				ForceSettings:      flags.forceSettings,
				SkipSearchIndex:    flags.skipSearchIndex,
			}

			interactive := !flags.nonInteractive && stdinIsTerminal(cmd.InOrStdin())
			var reader *bufio.Reader
			if interactive {
				reader = bufio.NewReader(cmd.InOrStdin())
				if err := promptRestoreOptions(cmd, reader, &options); err != nil {
					return err
				}
			}
			if err := options.Normalize(); err != nil {
				return err
			}
			if options.MediaWikiDir != "" && !options.ForceSettings {
				settingsPath := filepath.Join(options.MediaWikiDir, "LocalSettings.php")
				if _, err := os.Stat(settingsPath); err == nil {
					if !interactive {
						return fmt.Errorf("%s exists; use --force-settings to replace it", settingsPath)
					}
					replace, err := promptBool(cmd, reader, "Replace the checkout's existing LocalSettings.php?", false)
					if err != nil {
						return err
					}
					if !replace {
						return fmt.Errorf("restore cancelled without replacing LocalSettings.php")
					}
					options.ForceSettings = true
				}
			}
			if options.Target == restorer.TargetExisting && interactive && !flags.prepareOnly {
				proceed, err := promptBool(
					cmd, reader,
					fmt.Sprintf("Update schema, import data, and reset admin %q in database %q?", options.AdminUser, options.DBName),
					false,
				)
				if err != nil {
					return err
				}
				if !proceed {
					return fmt.Errorf("restore cancelled")
				}
			}
			if flags.prepareOnly {
				layout, err := restorer.Prepare(options)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Compose configuration: %s\n", layout.ComposePath)
				if layout.BaseComposePath != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Devcontainer Compose base: %s\n", layout.BaseComposePath)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "LocalSettings.php: %s\n", layout.SettingsPath)
				if layout.SettingsBackup != "" {
					if _, err := os.Stat(layout.SettingsBackup); err == nil {
						fmt.Fprintf(cmd.OutOrStdout(), "Previous LocalSettings.php backup: %s\n", layout.SettingsBackup)
					}
				}
				return nil
			}

			runner := restorer.Runner{
				Options: options,
				Out:     cmd.OutOrStdout(),
				Err:     cmd.ErrOrStderr(),
			}
			return runner.Run(cmd.Context())
		},
	}

	f := command.Flags()
	command.PersistentFlags().StringVar(&flags.stateDir, "state-dir", filepath.Join(".local", "restore"), "generated configuration, secrets, and lifecycle state directory")
	f.StringVar(&flags.target, "target", "new", "restore target: new or existing")
	f.StringVar(&flags.mediaWikiDir, "mediawiki-dir", "", "path to a 52poke/mediawiki checkout")
	f.StringVar(&flags.image, "image", "ghcr.io/52poke/mediawiki:latest", "MediaWiki image when no checkout is used")
	f.StringVar(&flags.containerCLI, "container-cli", "docker", "container CLI with Compose support")
	f.StringVar(&flags.projectName, "project-name", "mwarchiver-restore", "Compose project name")
	f.IntVar(&flags.port, "port", 8227, "localhost HTTP port")
	f.StringVar(&flags.dbHost, "db-host", "", "existing database host as seen from the container")
	f.IntVar(&flags.dbPort, "db-port", 3306, "database port")
	f.StringVar(&flags.dbName, "db-name", "52poke_wiki", "database name")
	f.StringVar(&flags.dbUser, "db-user", "mediawiki", "database application user")
	f.StringVar(&flags.dbPasswordFile, "db-password-file", "", "file containing the existing database password")
	f.StringVar(&flags.adminUser, "admin-user", "Admin", "administrator account to create or update")
	f.StringVar(&flags.adminEmail, "admin-email", "admin@example.invalid", "administrator email (required for OAuth)")
	f.BoolVar(&flags.elasticsearch, "elasticsearch", false, "enable persistent Elasticsearch and CirrusSearch")
	f.StringVar(&flags.elasticsearchImage, "elasticsearch-image", "ghcr.io/52poke/52w-elasticsearch:latest", "Elasticsearch image")
	f.StringVar(&flags.eventBusURL, "eventbus-url", "", "Timburr/EventBus HTTP endpoint; also enables local Kafka")
	f.BoolVar(&flags.oauth, "oauth", false, "enable OAuth and create an approved OAuth 1.0a client")
	f.StringVar(&flags.oauthName, "oauth-name", "Local development client", "OAuth client name")
	f.StringVar(&flags.oauthDescription, "oauth-description", "OAuth client created by mwarchiver restore", "OAuth client description")
	f.StringVar(&flags.oauthVersion, "oauth-version", "1.0", "OAuth client application version")
	f.StringVar(&flags.oauthCallback, "oauth-callback", "", "OAuth callback URL")
	f.StringSliceVar(&flags.oauthGrants, "oauth-grant", []string{"basic"}, "OAuth grant; may be repeated")
	f.BoolVar(&flags.forceSettings, "force-settings", false, "replace an existing checkout LocalSettings.php")
	f.BoolVar(&flags.nonInteractive, "non-interactive", false, "use flags and defaults without prompting")
	f.BoolVar(&flags.prepareOnly, "prepare-only", false, "generate configuration and secrets without starting containers")
	f.BoolVar(&flags.skipSearchIndex, "skip-search-index", false, "configure Elasticsearch without rebuilding its search index")
	command.AddCommand(
		newRestoreLifecycleCommand("up", restorer.LifecycleUp, "Start the prepared restore stack without importing again"),
		newRestoreLifecycleCommand("down", restorer.LifecycleDown, "Remove restore containers and networks while preserving volumes"),
		newRestoreLifecycleCommand("status", restorer.LifecycleStatus, "Show the prepared restore stack status"),
	)
	return command
}

func newRestoreLifecycleCommand(name string, action restorer.LifecycleAction, description string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, err := cmd.Flags().GetString("state-dir")
			if err != nil {
				return err
			}
			return restorer.RunLifecycle(
				cmd.Context(), stateDir, action,
				cmd.OutOrStdout(), cmd.ErrOrStderr(),
			)
		},
	}
}

func promptRestoreOptions(cmd *cobra.Command, reader *bufio.Reader, options *restorer.Options) error {
	var err error
	if !cmd.Flags().Changed("target") {
		value, err := promptChoice(cmd, reader, "Restore into a new or existing database?", string(options.Target), "new", "existing")
		if err != nil {
			return err
		}
		options.Target = restorer.Target(value)
	}
	if !cmd.Flags().Changed("mediawiki-dir") {
		options.MediaWikiDir, err = promptString(
			cmd, reader,
			"52poke/mediawiki checkout path (blank uses the published image)",
			options.MediaWikiDir,
		)
		if err != nil {
			return err
		}
	}
	if options.Target == restorer.TargetExisting {
		if !cmd.Flags().Changed("db-host") {
			options.DBHost, err = promptString(cmd, reader, "Existing database host", options.DBHost)
			if err != nil {
				return err
			}
		}
		if !cmd.Flags().Changed("db-password-file") {
			options.DBPasswordFile, err = promptString(cmd, reader, "Database password file", options.DBPasswordFile)
			if err != nil {
				return err
			}
		}
	}
	if !cmd.Flags().Changed("admin-user") {
		options.AdminUser, err = promptString(cmd, reader, "Administrator username", options.AdminUser)
		if err != nil {
			return err
		}
	}
	if !cmd.Flags().Changed("elasticsearch") {
		options.Elasticsearch, err = promptBool(cmd, reader, "Enable persistent Elasticsearch and CirrusSearch?", options.Elasticsearch)
		if err != nil {
			return err
		}
	}
	if !cmd.Flags().Changed("eventbus-url") {
		options.EventBusURL, err = promptString(cmd, reader, "Timburr/EventBus endpoint (blank disables EventBus and Kafka)", options.EventBusURL)
		if err != nil {
			return err
		}
	}
	if !cmd.Flags().Changed("oauth") {
		options.OAuth, err = promptBool(cmd, reader, "Enable OAuth and create a client?", options.OAuth)
		if err != nil {
			return err
		}
	}
	if options.OAuth {
		if !cmd.Flags().Changed("admin-email") {
			options.AdminEmail, err = promptString(cmd, reader, "Administrator email", options.AdminEmail)
			if err != nil {
				return err
			}
		}
		if !cmd.Flags().Changed("oauth-name") {
			options.OAuthName, err = promptString(cmd, reader, "OAuth client name", options.OAuthName)
			if err != nil {
				return err
			}
		}
		if !cmd.Flags().Changed("oauth-callback") {
			options.OAuthCallbackURL, err = promptString(cmd, reader, "OAuth callback URL", options.OAuthCallbackURL)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func promptString(cmd *cobra.Command, reader *bufio.Reader, label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", label)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s [%s]: ", label, defaultValue)
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

func promptChoice(cmd *cobra.Command, reader *bufio.Reader, label, defaultValue string, choices ...string) (string, error) {
	for {
		value, err := promptString(cmd, reader, fmt.Sprintf("%s (%s)", label, strings.Join(choices, "/")), defaultValue)
		if err != nil {
			return "", err
		}
		for _, choice := range choices {
			if strings.EqualFold(value, choice) {
				return choice, nil
			}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Choose one of: %s\n", strings.Join(choices, ", "))
	}
}

func promptBool(cmd *cobra.Command, reader *bufio.Reader, label string, defaultValue bool) (bool, error) {
	defaultText := "y/N"
	if defaultValue {
		defaultText = "Y/n"
	}
	for {
		value, err := promptString(cmd, reader, fmt.Sprintf("%s [%s]", label, defaultText), "")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "":
			return defaultValue, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(cmd.ErrOrStderr(), "Answer yes or no.")
		}
	}
}

func stdinIsTerminal(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return true
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func init() {
	rootCmd.AddCommand(newRestoreCommand())
}
