package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/matroidbe/pgbrew/internal/bottle"
	"github.com/matroidbe/pgbrew/internal/pgconf"
	"github.com/matroidbe/pgbrew/internal/sysdeps"
)

// configureServer controls whether pgbrew writes the PostgreSQL configuration
// an extension declares.
var configureServer bool

// planFromManifest builds a configuration plan from a source manifest.
func planFromManifest(extension string, section sysdeps.PostgresSection) pgconf.Plan {
	return pgconf.Plan{
		Extension:       extension,
		PreloadLibrary:  section.PreloadName(extension),
		Settings:        section.Settings,
		RestartRequired: section.RestartRequired,
	}
}

// planFromBottle builds a configuration plan from a bottle's manifest, so an
// install with no source tree still knows what the extension needs.
func planFromBottle(m bottle.Manifest) pgconf.Plan {
	if m.Postgres == nil {
		return pgconf.Plan{Extension: m.Name}
	}
	library := ""
	if m.Postgres.SharedPreloadLibraries {
		library = m.Postgres.Library
		if library == "" {
			library = m.Name
		}
	}
	return pgconf.Plan{
		Extension:       m.Name,
		PreloadLibrary:  library,
		Settings:        m.Postgres.Settings,
		RestartRequired: m.Postgres.RestartRequired,
	}
}

// handlePostgresConfig reports the configuration an extension needs and, with
// --configure, writes it.
//
// Reporting is the default because this changes a database server's
// configuration, and on a machine that is not the user's demo box that is not a
// decision pgbrew should make silently.
func handlePostgresConfig(plan pgconf.Plan) error {
	if plan.IsEmpty() {
		return nil
	}

	fmt.Println()
	fmt.Printf("%s needs PostgreSQL configuration:\n", plan.Extension)
	fmt.Print(plan.Describe())

	if !configureServer {
		fmt.Println()
		fmt.Println("  Not applied. Re-run with --configure to write it, or set it yourself.")
		if plan.PreloadLibrary != "" {
			fmt.Printf("  Without %s in %s, the extension will load but its\n",
				plan.PreloadLibrary, pgconf.PreloadSetting)
			fmt.Println("  background workers will not start.")
		}
		return nil
	}

	pgMajor := getPgVersion()
	layout, err := pgconf.Locate(pgMajor)
	if err != nil {
		return err
	}

	result, err := pgconf.Apply(plan, layout, useSudo)
	if err != nil {
		return err
	}

	fmt.Println()
	for _, path := range result.Written {
		fmt.Printf("  wrote %s\n", path)
	}
	if result.IncludeDirAdded {
		fmt.Printf("  added include_dir = '%s' to %s\n", pgconf.ConfDirName, layout.ConfigFile)
	}
	if len(result.PreloadLibraries) > 0 {
		fmt.Printf("  %s = %s\n", pgconf.PreloadSetting,
			pgconf.FormatPreloadList(result.PreloadLibraries))
	}

	printReloadInstruction(result.RestartRequired, pgMajor)
	return nil
}

// removePostgresConfig takes an extension's configuration back out.
func removePostgresConfig(extension string) {
	pgMajor := getPgVersion()
	layout, err := pgconf.Locate(pgMajor)
	if err != nil {
		// Not finding the config is not a reason to fail an uninstall.
		return
	}

	result, err := pgconf.Remove(extension, "", layout, useSudo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not remove PostgreSQL configuration: %v\n", err)
		return
	}
	if len(result.Written) == 0 {
		return
	}

	for _, path := range result.Written {
		fmt.Printf("  removed %s\n", path)
	}
	// A stale entry here is not cosmetic: PostgreSQL refuses to start if
	// shared_preload_libraries names a library that is no longer installed.
	printReloadInstruction(result.RestartRequired, pgMajor)
}

// printReloadInstruction tells the user how to make the change take effect.
//
// pgbrew never restarts a database itself. On anything but a scratch box that
// is an outage, and it is not a decision a package manager should take.
func printReloadInstruction(restart bool, pgMajor string) {
	fmt.Println()
	if restart {
		fmt.Println("  A restart is required for this to take effect:")
		if pgMajor != "" && isDebianish() {
			fmt.Printf("    sudo pg_ctlcluster %s main restart\n", pgMajor)
		} else {
			fmt.Println("    sudo systemctl restart postgresql")
		}
	} else {
		fmt.Println("  Reload for this to take effect:")
		fmt.Println("    SELECT pg_reload_conf();")
	}
}

func isDebianish() bool {
	_, err := os.Stat("/etc/debian_version")
	return err == nil
}

// preloadWarning is the fallback for extensions with no manifest declaration.
//
// The heuristic it relies on greps the source for background-worker types, so
// it cannot work for a bottle and can misjudge a C extension. It stays as a
// safety net for extensions that have not declared anything, but a declaration
// in the manifest is what should be relied on.
func preloadWarning(extName string, detected bool) {
	if !detected {
		return
	}
	fmt.Println()
	fmt.Println("⚠ This extension appears to use background workers.")
	fmt.Printf("  It probably needs to be in %s:\n", pgconf.PreloadSetting)
	fmt.Printf("    %s = '%s'\n", pgconf.PreloadSetting, extName)
	fmt.Println("  Then restart PostgreSQL.")
	fmt.Println()
	fmt.Println("  This was inferred from the source. Declaring it in the extension's")
	fmt.Println("  pgbrew manifest would let pgbrew apply it for you:")
	fmt.Println()
	fmt.Println("    [postgresql]")
	fmt.Println("    shared_preload_libraries = true")
}

// describePlanForBottle is a small helper so bottle creation can report what
// configuration it captured.
func describePlanForBottle(section sysdeps.PostgresSection, extension string) string {
	if section.IsZero() {
		return ""
	}
	var b strings.Builder
	b.WriteString("  PostgreSQL configuration captured:\n")
	b.WriteString(planFromManifest(extension, section).Describe())
	return b.String()
}
