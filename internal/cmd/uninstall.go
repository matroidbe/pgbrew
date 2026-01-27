package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/spf13/cobra"
)

var (
	uninstallDryRun bool
	uninstallUseSudo bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <extension>",
	Short: "Uninstall an extension",
	Long: `Uninstall a PostgreSQL extension.

Note: This removes the extension files but does not DROP the extension from databases.
You should run DROP EXTENSION in each database before uninstalling.

Examples:
  pgx uninstall pg_kafka
  pgx uninstall --sudo pg_kafka  # Uninstall with sudo for system PostgreSQL`,
	Args: cobra.ExactArgs(1),
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Show what would be removed without deleting")
	uninstallCmd.Flags().BoolVar(&uninstallUseSudo, "sudo", false, "Use sudo for uninstallation (needed for system PostgreSQL)")
}

func runUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Set sudo mode for cellar operations
	cellar.SetUseSudo(uninstallUseSudo)

	// Check if extension is tracked by pgx
	entry, err := cellar.Get(name)
	tracked := err == nil

	if uninstallDryRun {
		fmt.Printf("Dry run: would uninstall %s\n\n", name)
	} else if tracked {
		fmt.Printf("Uninstalling %s %s...\n", entry.Name, entry.Version)
	} else {
		fmt.Printf("Uninstalling %s...\n", name)
	}

	// Get PostgreSQL directories
	pgConfigPath := getPgConfigPath()
	libDir := strings.TrimSpace(getCommandOutput(pgConfigPath, "--pkglibdir"))
	shareDir := strings.TrimSpace(getCommandOutput(pgConfigPath, "--sharedir"))

	if libDir == "" || shareDir == "" {
		return fmt.Errorf("could not determine PostgreSQL directories")
	}

	extDir := filepath.Join(shareDir, "extension")

	// Find extension files
	var files []string

	// .so file from lib directory
	soFile := filepath.Join(libDir, name+".so")
	if _, err := os.Stat(soFile); err == nil {
		files = append(files, soFile)
	}

	// .control file
	controlFile := filepath.Join(extDir, name+".control")
	if _, err := os.Stat(controlFile); err == nil {
		files = append(files, controlFile)
	}

	// SQL files (pattern: name--*.sql)
	sqlPattern := filepath.Join(extDir, name+"--*.sql")
	sqlFiles, _ := filepath.Glob(sqlPattern)
	files = append(files, sqlFiles...)

	// Also try name.sql (some extensions use this)
	sqlFile := filepath.Join(extDir, name+".sql")
	if _, err := os.Stat(sqlFile); err == nil {
		files = append(files, sqlFile)
	}

	if len(files) == 0 {
		fmt.Println("No extension files found.")
		return nil
	}

	// Check which databases have this extension installed
	activeDbs := findDatabasesWithExtension(name)

	// Dry run: just show what would be removed
	if uninstallDryRun {
		if len(activeDbs) > 0 {
			fmt.Printf("Extension is active in %d database(s):\n", len(activeDbs))
			for _, db := range activeDbs {
				fmt.Printf("  - %s\n", db)
			}
			fmt.Println()
		}

		fmt.Printf("Would remove %d files:\n", len(files))
		for _, f := range files {
			fmt.Printf("  - %s\n", f)
		}
		if tracked {
			fmt.Println("\nWould remove from pgx tracking.")
		}
		return nil
	}

	// Block uninstall if extension is active in any database
	if len(activeDbs) > 0 {
		fmt.Printf("Error: Extension is active in %d database(s):\n", len(activeDbs))
		for _, db := range activeDbs {
			fmt.Printf("  - %s\n", db)
		}
		fmt.Println()
		fmt.Println("Run DROP EXTENSION in each database first:")
		for _, db := range activeDbs {
			psqlPath := getPsqlPath()
			fmt.Printf("  %s -d %s -c \"DROP EXTENSION IF EXISTS %s;\"\n", psqlPath, db, name)
		}
		return fmt.Errorf("cannot uninstall: extension is still active")
	}

	// Actually remove files
	var removed []string
	for _, f := range files {
		var err error
		if uninstallUseSudo {
			// Use sudo to remove file
			rmCmd := exec.Command("sudo", "rm", "-f", f)
			err = rmCmd.Run()
		} else {
			err = os.Remove(f)
		}
		if err == nil {
			removed = append(removed, f)
		}
	}

	// Remove from cellar tracking if it was tracked
	if tracked {
		if err := cellar.Remove(name); err != nil {
			return fmt.Errorf("failed to remove from cellar: %w", err)
		}
	}

	if len(removed) > 0 {
		fmt.Printf("✓ Removed %d files:\n", len(removed))
		for _, f := range removed {
			fmt.Printf("  - %s\n", f)
		}
	}

	return nil
}

// findDatabasesWithExtension returns a list of database names that have the extension installed
func findDatabasesWithExtension(extName string) []string {
	// Get psql path from the same PostgreSQL installation
	psqlPath := getPsqlPath()

	// First, get list of all databases (connect to postgres db)
	cmd := exec.Command(psqlPath, "-t", "-A", "-d", "postgres", "-c", "SELECT datname FROM pg_database WHERE datistemplate = false")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	databases := strings.Split(strings.TrimSpace(string(output)), "\n")
	var activeDbs []string

	// Check each database for the extension
	for _, db := range databases {
		db = strings.TrimSpace(db)
		if db == "" {
			continue
		}

		query := fmt.Sprintf("SELECT 1 FROM pg_extension WHERE extname = '%s'", extName)
		cmd := exec.Command(psqlPath, "-t", "-A", "-d", db, "-c", query)
		output, err := cmd.Output()
		if err != nil {
			continue
		}

		if strings.TrimSpace(string(output)) == "1" {
			activeDbs = append(activeDbs, db)
		}
	}

	return activeDbs
}

// getPsqlPath returns the path to psql, deriving it from PG_CONFIG if set
func getPsqlPath() string {
	if pgConfig := os.Getenv("PG_CONFIG"); pgConfig != "" {
		// pg_config is usually in the same bin directory as psql
		binDir := filepath.Dir(pgConfig)
		psqlPath := filepath.Join(binDir, "psql")
		if _, err := os.Stat(psqlPath); err == nil {
			return psqlPath
		}
	}
	return "psql"
}
