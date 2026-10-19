package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/matroidbe/pgbrew/internal/builder"
	"github.com/matroidbe/pgbrew/internal/cellar"
	"github.com/matroidbe/pgbrew/internal/github"
	"github.com/spf13/cobra"
)

var (
	updateBottle        string
	updateApply         bool
	updateDatabasesOnly bool
)

var updateCmd = &cobra.Command{
	Use:   "update <extension>[@ref]",
	Short: "Update an installed extension to a newer version",
	Long: `Update an extension pgx installed, from where it was installed, then bring
each database that uses it up to the new version.

The files are replaced the way an install replaces them: a running server keeps
the library it already loaded until its sessions reconnect (or, for a preloaded
library, until it restarts). The databases are a separate step, because
ALTER EXTENSION ... UPDATE runs the extension's upgrade scripts against them:

  pgx update pg_cron                         # rebuild from the recorded source
  pgx update vector@v0.8.8                   # a GitHub source, at another tag or branch
  pgx update pg_kafka --bottle <file-or-url> # a newer bottle
  pgx update vector --apply                  # ...then ALTER EXTENSION ... UPDATE everywhere
  pgx update age --databases-only            # only the ALTER step (e.g. after a restart)

A source install is rebuilt with the --features and --build-var it was
installed with, unless new ones are given.`,
	Args: cobra.ExactArgs(1),
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().StringVar(&updateBottle, "bottle", "", "Update from this bottle (file path or http(s) URL)")
	updateCmd.Flags().BoolVar(&updateApply, "apply", false, "Run ALTER EXTENSION ... UPDATE in every database that has the extension")
	updateCmd.Flags().BoolVar(&updateDatabasesOnly, "databases-only", false, "Install nothing; only update the extension in each database")
	updateCmd.Flags().BoolVar(&useSudo, "sudo", false, "Use sudo for installation (needed for system PostgreSQL)")
	updateCmd.Flags().StringSliceVar(&features, "features", nil, "Cargo features to build with (pgrx; default: the ones it was installed with)")
	updateCmd.Flags().StringArrayVar(&buildVars, "build-var", nil, buildVarHelp+" (default: the ones it was installed with)")
	rootCmd.AddCommand(updateCmd)
}

// sqlName is what pgx will interpolate into SQL as an extension name or a
// version. Both come from the control file or the user, never from a query.
var sqlName = regexp.MustCompile(`^[A-Za-z0-9_.+-]+$`)

func runUpdate(cmd *cobra.Command, args []string) error {
	name, ref, _ := strings.Cut(args[0], "@")
	if !sqlName.MatchString(name) {
		return fmt.Errorf("invalid extension name %q", name)
	}
	if err := builder.ValidateBuildVars(buildVars); err != nil {
		return fmt.Errorf("--build-var: %w", err)
	}
	cellar.SetUseSudo(useSudo)

	if updateDatabasesOnly {
		if ref != "" || updateBottle != "" {
			return fmt.Errorf("--databases-only installs nothing; do not also pass @ref or --bottle")
		}
		return updateDatabases(name, false)
	}

	entry, err := cellar.Get(name)
	if err != nil {
		return fmt.Errorf("%s was not installed by pgx, so there is no source to update it from; use pgx install", name)
	}
	plan, err := planUpdate(*entry, ref, updateBottle)
	if err != nil {
		return err
	}

	if plan.bottle != "" {
		if err := installFromBottle(plan.bottle, name); err != nil {
			return err
		}
	} else {
		if !cmd.Flags().Changed("features") {
			features = entry.Features
		}
		if !cmd.Flags().Changed("build-var") {
			buildVars = entry.BuildVars
		}
		if err := installFromSource(plan.source); err != nil {
			return err
		}
	}

	after, err := cellar.Get(name)
	if err == nil && after.Version != entry.Version {
		fmt.Printf("\n✓ Updated %s %s → %s\n", name, entry.Version, after.Version)
	} else {
		fmt.Printf("\n✓ Rebuilt %s; the version is still %s\n", name, entry.Version)
	}
	return updateDatabases(name, true)
}

// updateSource is where an update installs from: exactly one of a bottle or a
// source (a GitHub reference or a local directory) is set.
type updateSource struct {
	bottle string
	source string
}

// planUpdate decides where an update installs from, given what the cellar
// recorded for the install and what the user asked for.
func planUpdate(entry cellar.Entry, ref, bottle string) (updateSource, error) {
	if bottle != "" {
		if ref != "" {
			return updateSource{}, fmt.Errorf("pass either @ref or --bottle, not both")
		}
		return updateSource{bottle: bottle}, nil
	}

	if cellar.IsBottleSource(entry.Source) {
		return updateSource{}, fmt.Errorf(
			"%s was installed from a bottle (%s), which names one version.\n"+
				"Name the newer bottle: pgx update %s --bottle <file-or-url>",
			entry.Name, entry.Source, entry.Name)
	}

	if !strings.HasPrefix(strings.TrimPrefix(entry.Source, "https://"), "github.com/") {
		if ref != "" {
			return updateSource{}, fmt.Errorf(
				"%s was built from the local directory %s; check out the version there and run pgx update %s",
				entry.Name, entry.Source, entry.Name)
		}
		if info, err := os.Stat(entry.Source); err != nil || !info.IsDir() {
			return updateSource{}, fmt.Errorf("%s was built from %s, which no longer exists", entry.Name, entry.Source)
		}
		return updateSource{source: entry.Source}, nil
	}

	repo, subpath, oldRef, err := github.ParseURL(entry.Source)
	if err != nil {
		return updateSource{}, fmt.Errorf("recorded source %q: %w", entry.Source, err)
	}
	if ref == "" {
		if oldRef != "" {
			// Rebuilding a pinned tag would build the same version again, and
			// guessing "the latest tag" would pick a version nobody chose.
			return updateSource{}, fmt.Errorf(
				"%s was installed at @%s; name the version to update to: pgx update %s@<tag>",
				entry.Name, oldRef, entry.Name)
		}
		ref = oldRef
	}
	source := repo
	if subpath != "" {
		source += "/" + subpath
	}
	if ref != "" {
		source += "@" + ref
	}
	return updateSource{source: source}, nil
}

// dbStatus is one database's copy of an extension, against the version the
// installed files now provide.
type dbStatus struct {
	db        string
	installed string
	hasPath   bool // an upgrade-script path leads from installed to the target
}

// updateDatabases reports, and with --apply or --databases-only runs, the
// ALTER EXTENSION ... UPDATE each database needs. justInstalled says the
// library files changed in this run, which matters for a preloaded library.
func updateDatabases(name string, justInstalled bool) error {
	target, err := psqlValue("postgres", fmt.Sprintf(
		"SELECT default_version FROM pg_available_extensions WHERE name = '%s'", name))
	if err != nil {
		fmt.Printf("\nCould not reach PostgreSQL to check the databases (%v).\n", err)
		fmt.Printf("Update each one with: ALTER EXTENSION \"%s\" UPDATE;\n", name)
		return nil
	}
	if target == "" {
		return fmt.Errorf("PostgreSQL has no %s extension available", name)
	}
	if !sqlName.MatchString(target) {
		return fmt.Errorf("unexpected default_version %q for %s", target, name)
	}

	statuses, err := databasesWith(name, target)
	if err != nil {
		return err
	}
	var pending, stuck []dbStatus
	for _, s := range statuses {
		switch {
		case s.installed == target:
		case s.hasPath:
			pending = append(pending, s)
		default:
			stuck = append(stuck, s)
		}
	}

	if len(statuses) == 0 {
		fmt.Printf("\nNo database has %s created yet; CREATE EXTENSION gets %s.\n", name, target)
	} else if len(pending) == 0 && len(stuck) == 0 {
		fmt.Printf("\nEvery database with %s is already at %s.\n", name, target)
	}

	for _, s := range stuck {
		fmt.Printf("\n! %s: %s %s has no upgrade path to %s (the extension ships no script for it).\n"+
			"  Updating needs DROP EXTENSION %s CASCADE and CREATE EXTENSION again, which drops\n"+
			"  every object that depends on it. pgx will not do that for you.\n",
			s.db, name, s.installed, target, name)
	}

	preload := isPreloaded(name)
	if len(pending) > 0 {
		apply := updateApply || updateDatabasesOnly
		switch {
		case apply && preload && justInstalled:
			// The server still runs the old library. Upgrade scripts that
			// reference new C functions would fail, or bind to the old code.
			fmt.Printf("\n%s is preloaded, so the server still runs the old library.\n"+
				"Restart PostgreSQL, then run: pgx update %s --databases-only\n", name, name)
			return nil
		case apply:
			fmt.Println()
			for _, s := range pending {
				if _, err := psqlValue(s.db, fmt.Sprintf("ALTER EXTENSION \"%s\" UPDATE TO '%s'", name, target)); err != nil {
					return fmt.Errorf("%s: ALTER EXTENSION %s UPDATE failed: %w", s.db, name, err)
				}
				fmt.Printf("✓ %s: %s %s → %s\n", s.db, name, s.installed, target)
			}
		default:
			fmt.Printf("\n%d database(s) still have an older %s:\n", len(pending), name)
			for _, s := range pending {
				fmt.Printf("  %s: %s\n", s.db, s.installed)
			}
			if preload && justInstalled {
				fmt.Printf("Restart PostgreSQL (%s is preloaded), then run: pgx update %s --databases-only\n", name, name)
			} else {
				fmt.Printf("Update them with: pgx update %s --databases-only\n", name)
			}
		}
	}

	if justInstalled && len(pending) == 0 {
		if preload {
			fmt.Printf("\n%s is preloaded: restart PostgreSQL to load the new library.\n", name)
		} else {
			fmt.Println("\nNew sessions load the new library; open sessions keep the old one until they reconnect.")
		}
	}
	return nil
}

// databasesWith lists every database that has the extension created, with its
// version and whether an upgrade path leads to target. Templates count: a
// template1 left on the old version hands it to every new database.
func databasesWith(name, target string) ([]dbStatus, error) {
	out, err := psqlValue("postgres", "SELECT datname FROM pg_database WHERE datallowconn ORDER BY datname")
	if err != nil {
		return nil, err
	}
	var statuses []dbStatus
	for _, db := range strings.Split(out, "\n") {
		if db == "" {
			continue
		}
		row, err := psqlValue(db, fmt.Sprintf(`SELECT e.extversion, coalesce((
			SELECT p.path IS NOT NULL FROM pg_extension_update_paths('%s') p
			WHERE p.source = e.extversion AND p.target = '%s'), false)
			FROM pg_extension e WHERE e.extname = '%s'`, name, target, name))
		if err != nil || row == "" {
			continue // no access to that database, or no extension in it
		}
		version, hasPath, _ := strings.Cut(row, "|")
		statuses = append(statuses, dbStatus{db: db, installed: version, hasPath: hasPath == "t"})
	}
	return statuses, nil
}

// isPreloaded reports whether shared_preload_libraries names the extension's
// library (the extension's own name, the convention every preloaded one here
// follows).
func isPreloaded(name string) bool {
	out, err := psqlValue("postgres", "SHOW shared_preload_libraries")
	if err != nil {
		return false
	}
	for _, lib := range strings.Split(out, ",") {
		lib = strings.Trim(strings.TrimSpace(lib), `"'`)
		lib = strings.TrimPrefix(lib, "$libdir/")
		if lib == name {
			return true
		}
	}
	return false
}

// psqlValue runs one statement in db and returns its unaligned, tuples-only
// output.
func psqlValue(db, sql string) (string, error) {
	cmd := exec.Command(getPsqlPath(), "-X", "-At", "-v", "ON_ERROR_STOP=1", "-d", db, "-c", sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
