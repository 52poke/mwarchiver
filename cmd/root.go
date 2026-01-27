package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/mudkipme/mwarchiver/internal/archiver"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var config struct {
	APIURL     string `mapstructure:"api_url"`
	UserAgent  string `mapstructure:"user_agent"`
	DBPath     string `mapstructure:"db_path"`
	OutputPath string `mapstructure:"output_path"`
	Namespaces []int  `mapstructure:"namespaces"`
	Limit      int    `mapstructure:"limit"`
}

var rootCmd = &cobra.Command{
	Use:   "mwarchiver",
	Short: "A MediaWiki archiver",
	Long:  "This program is intended to export articles from a MediaWiki instance to a SQLite database.",
	Run: func(cmd *cobra.Command, args []string) {
		dbPath := config.DBPath
		if dbPath == "" {
			dbPath = config.OutputPath
		}
		archiver, err := archiver.NewArchiver(dbPath, config.APIURL, config.UserAgent)
		cobra.CheckErr(err)
		defer func() {
			if err := archiver.Close(); err != nil {
				slog.Error("Failed to close archive database", slog.String("error", err.Error()))
			}
		}()
		for _, namespace := range config.Namespaces {
			slog.Info("Archiving namespace", slog.Int("namespace", namespace))
			err := archiver.ArchiveNamespace(namespace, config.Limit)
			cobra.CheckErr(err)
		}
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.mwarchiver.yaml)")
}

func initConfig() {
	viper.SetEnvPrefix("MWARCHIVER")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	_ = viper.BindEnv("api_url")
	_ = viper.BindEnv("user_agent")
	_ = viper.BindEnv("db_path")
	_ = viper.BindEnv("output_path")
	_ = viper.BindEnv("namespaces")
	_ = viper.BindEnv("limit")

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.SetDefault("api_url", "https://en.wikipedia.org/w/api.php")
		viper.SetDefault("user_agent", "mwarchiver 1.0")
		viper.SetDefault("db_path", "mwarchiver.db")
		viper.SetDefault("output_path", "")
		viper.SetDefault("namespaces", []int{0})
		viper.SetDefault("limit", 100)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".mwarchiver")
	}

	if namespacesRaw := os.Getenv("MWARCHIVER_NAMESPACES"); namespacesRaw != "" {
		parts := strings.Split(namespacesRaw, ",")
		namespaces := make([]int, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			value, err := strconv.Atoi(part)
			if err != nil {
				cobra.CheckErr(fmt.Errorf("invalid MWARCHIVER_NAMESPACES value %q: %w", namespacesRaw, err))
			}
			namespaces = append(namespaces, value)
		}
		if len(namespaces) > 0 {
			viper.Set("namespaces", namespaces)
		}
	}

	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	} else if cfgFile != "" {
		cobra.CheckErr(err)
	}

	err := viper.Unmarshal(&config)
	cobra.CheckErr(err)
}
