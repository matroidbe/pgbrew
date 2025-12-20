package cmd

import (
	"fmt"

	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed extensions",
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	entries, err := cellar.List()
	if err != nil {
		return fmt.Errorf("failed to list extensions: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No extensions installed via pgx.")
		return nil
	}

	fmt.Println("Installed extensions:")
	fmt.Println()
	for _, e := range entries {
		fmt.Printf("  %s %s (pg%s)\n", e.Name, e.Version, e.PgVersion)
		fmt.Printf("    Source: %s\n", e.Source)
	}

	return nil
}
