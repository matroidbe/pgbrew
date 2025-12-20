package cmd

import (
	"github.com/spf13/cobra"
)

var (
	// Version is set at build time
	Version = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "pgx",
	Short: "PostgreSQL extension package manager",
	Long: `pgbrew (pgx) is a Homebrew-inspired package manager for PostgreSQL extensions.

Install extensions directly from GitHub repositories:
  pgx install github.com/user/repo

Manage installed extensions:
  pgx list
  pgx info <extension>
  pgx uninstall <extension>

Check your system:
  pgx doctor`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(upgradeCmd)
}
