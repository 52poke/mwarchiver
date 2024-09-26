package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/mudkipme/mwarchiver/internal/archiver"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var config struct {
	APIURL     string `mapstructure:"api_url"`
	OutputPath string `mapstructure:"output_path"`
	Namespaces []int  `mapstructure:"namespaces"`
	Limit      int    `mapstructure:"limit"`
}

var rootCmd = &cobra.Command{
	Use:   "mwarchiver",
	Short: "A MediaWiki archiver",
	Long:  "This program is intended to export articles from a MediaWiki instance to `.txt` files.",
	Run: func(cmd *cobra.Command, args []string) {
		archiver := archiver.NewArchiver(config.OutputPath, config.APIURL)
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
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.SetDefault("api_url", "https://en.wikipedia.org/w/api.php")
		viper.SetDefault("output_path", "output")
		viper.SetDefault("namespaces", []int{0})
		viper.SetDefault("limit", 100)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".mwarchiver")
	}

	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	err := viper.Unmarshal(&config)
	cobra.CheckErr(err)
}
