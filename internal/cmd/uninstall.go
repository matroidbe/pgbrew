package cmd

import (
	"fmt"

	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <extension>",
	Short: "Uninstall an extension",
	Long: `Uninstall a PostgreSQL extension.

Note: This removes the extension files but does not DROP the extension from databases.
You should run DROP EXTENSION in each database before uninstalling.`,
	Args: cobra.ExactArgs(1),
	RunE: runUninstall,
}

func runUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]

	entry, err := cellar.Get(name)
	if err != nil {
		return fmt.Errorf("extension not found: %s", name)
	}

	fmt.Printf("Uninstalling %s %s...\n", entry.Name, entry.Version)
	fmt.Println()
	fmt.Println("Warning: Make sure to run DROP EXTENSION in all databases first:")
	fmt.Printf("  DROP EXTENSION IF EXISTS %s;\n", name)
	fmt.Println()

	// Remove from cellar tracking
	if err := cellar.Remove(name); err != nil {
		return fmt.Errorf("failed to remove from cellar: %w", err)
	}

	// TODO: Actually remove extension files from PostgreSQL directories
	// This requires knowing the exact files installed (lib/*.so, share/extension/*.control, *.sql)
	// For now, we just remove the tracking entry

	fmt.Printf("✓ Removed %s from pgbrew tracking\n", name)
	fmt.Println("  Note: Extension files may still exist in PostgreSQL directories")

	return nil
}
