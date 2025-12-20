package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/spf13/cobra"
)

var listAll bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed extensions",
	Long: `List extensions installed via pgx.

Use --all to show all PostgreSQL extensions, including those not installed via pgx.`,
	RunE: runList,
}

func init() {
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all PostgreSQL extensions, not just pgx-installed")
}

func runList(cmd *cobra.Command, args []string) error {
	if listAll {
		return runListAll()
	}

	entries, err := cellar.List()
	if err != nil {
		return fmt.Errorf("failed to list extensions: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No extensions installed via pgx.")
		fmt.Println("Use --all to see all PostgreSQL extensions.")
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

// extensionInfo holds parsed information from a .control file
type extensionInfo struct {
	Name    string
	Version string
	Comment string
	ViaPgx  bool
}

func runListAll() error {
	// Get extension directory from pg_config
	pgConfigPath := getPgConfigPath()
	shareDir := strings.TrimSpace(getCommandOutput(pgConfigPath, "--sharedir"))
	if shareDir == "" {
		return fmt.Errorf("could not determine PostgreSQL share directory (is pg_config available?)")
	}

	extDir := filepath.Join(shareDir, "extension")

	// Check if extension directory exists
	if _, err := os.Stat(extDir); os.IsNotExist(err) {
		fmt.Printf("Extension directory not found: %s\n", extDir)
		return nil
	}

	// Get list of extensions installed via pgx
	pgxExtensions := make(map[string]bool)
	if entries, err := cellar.List(); err == nil {
		for _, e := range entries {
			pgxExtensions[e.Name] = true
		}
	}

	// Scan for .control files
	controlFiles, err := filepath.Glob(filepath.Join(extDir, "*.control"))
	if err != nil {
		return fmt.Errorf("failed to scan extension directory: %w", err)
	}

	if len(controlFiles) == 0 {
		fmt.Println("No extensions found.")
		return nil
	}

	// Parse each control file
	var extensions []extensionInfo
	for _, controlFile := range controlFiles {
		ext := parseControlFile(controlFile)
		ext.ViaPgx = pgxExtensions[ext.Name]
		extensions = append(extensions, ext)
	}

	// Display results
	fmt.Printf("Found %d extensions in %s:\n\n", len(extensions), extDir)

	var pgxCount, externalCount int
	for _, ext := range extensions {
		marker := " "
		if ext.ViaPgx {
			marker = "*"
			pgxCount++
		} else {
			externalCount++
		}

		version := ext.Version
		if version == "" {
			version = "-"
		}

		if ext.Comment != "" {
			fmt.Printf("  %s %-25s %-10s %s\n", marker, ext.Name, version, ext.Comment)
		} else {
			fmt.Printf("  %s %-25s %s\n", marker, ext.Name, version)
		}
	}

	fmt.Println()
	fmt.Printf("Total: %d extensions (%d via pgx, %d external)\n", len(extensions), pgxCount, externalCount)
	fmt.Println("  * = installed via pgx")

	return nil
}

// parseControlFile reads a .control file and extracts extension metadata
func parseControlFile(path string) extensionInfo {
	ext := extensionInfo{
		Name: strings.TrimSuffix(filepath.Base(path), ".control"),
	}

	file, err := os.Open(path)
	if err != nil {
		return ext
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key = value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// Remove quotes
		value = strings.Trim(value, "'\"")

		switch key {
		case "default_version":
			ext.Version = value
		case "comment":
			ext.Comment = value
		}
	}

	return ext
}
