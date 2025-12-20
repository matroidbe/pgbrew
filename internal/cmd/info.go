package cmd

import (
	"fmt"

	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <extension>",
	Short: "Show extension information",
	Args:  cobra.ExactArgs(1),
	RunE:  runInfo,
}

func runInfo(cmd *cobra.Command, args []string) error {
	name := args[0]

	entry, err := cellar.Get(name)
	if err != nil {
		return fmt.Errorf("extension not found: %s", name)
	}

	fmt.Printf("Name:        %s\n", entry.Name)
	fmt.Printf("Version:     %s\n", entry.Version)
	fmt.Printf("Source:      %s\n", entry.Source)
	fmt.Printf("PostgreSQL:  %s\n", entry.PgVersion)
	fmt.Printf("Installed:   %s\n", entry.InstalledAt.Format("2006-01-02 15:04:05"))

	return nil
}
