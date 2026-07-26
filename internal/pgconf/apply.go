package pgconf

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ConfDirName is the drop-in directory pgbrew creates beside postgresql.conf.
const ConfDirName = "conf.d"

// Plan is the configuration one extension needs.
type Plan struct {
	Extension string
	// PreloadLibrary is the name to add to shared_preload_libraries, or empty.
	PreloadLibrary  string
	Settings        map[string]string
	RestartRequired bool
}

// IsEmpty reports whether there is nothing to do.
func (p Plan) IsEmpty() bool {
	return p.PreloadLibrary == "" && len(p.Settings) == 0
}

// NeedsRestart reports whether applying the plan requires a server restart
// rather than a reload.
func (p Plan) NeedsRestart() bool {
	return p.PreloadLibrary != "" || p.RestartRequired
}

// Describe renders the plan for display, so the user can see exactly what
// would be changed before agreeing to it.
func (p Plan) Describe() string {
	var b strings.Builder
	if p.PreloadLibrary != "" {
		fmt.Fprintf(&b, "  %s += %s\n", PreloadSetting, p.PreloadLibrary)
	}
	keys := make([]string, 0, len(p.Settings))
	for k := range p.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s = %s\n", k, quoteValue(p.Settings[k]))
	}
	return b.String()
}

// Layout is where a PostgreSQL installation keeps its configuration.
type Layout struct {
	// ConfigFile is the path to postgresql.conf.
	ConfigFile string
	// ConfDir is the drop-in directory beside it.
	ConfDir string
}

// Result records what applying a plan changed.
type Result struct {
	Written         []string
	IncludeDirAdded bool
	RestartRequired bool
	// PreloadLibraries is the merged list after the change, for display.
	PreloadLibraries []string
}

// Locate finds a PostgreSQL installation's configuration file.
//
// pg_config does not report it, so this asks a running server first (the only
// authoritative answer) and falls back to the conventional locations.
func Locate(pgMajor string) (Layout, error) {
	if path := os.Getenv("PGBREW_POSTGRESQL_CONF"); path != "" {
		return layoutFor(path), nil
	}

	if path, ok := configFileFromServer(); ok {
		return layoutFor(path), nil
	}

	var candidates []string
	if pgMajor != "" {
		// Debian/Ubuntu keep config separate from data.
		candidates = append(candidates,
			filepath.Join("/etc/postgresql", pgMajor, "main", "postgresql.conf"))
	}
	if data := os.Getenv("PGDATA"); data != "" {
		candidates = append(candidates, filepath.Join(data, "postgresql.conf"))
	}
	if pgMajor != "" {
		candidates = append(candidates,
			filepath.Join("/var/lib/pgsql", pgMajor, "data", "postgresql.conf"))
	}
	candidates = append(candidates,
		"/var/lib/postgresql/data/postgresql.conf",
		"/usr/local/pgsql/data/postgresql.conf",
	)

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return layoutFor(path), nil
		}
	}

	return Layout{}, fmt.Errorf(
		"could not find postgresql.conf; set PGBREW_POSTGRESQL_CONF to its path")
}

func layoutFor(configFile string) Layout {
	return Layout{
		ConfigFile: configFile,
		ConfDir:    filepath.Join(filepath.Dir(configFile), ConfDirName),
	}
}

// configFileFromServer asks a running server where its config file is. This is
// the only fully reliable answer, since the path is a startup option.
func configFileFromServer() (string, bool) {
	cmd := exec.Command("psql", "-XtAc", "SHOW config_file")
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", false
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

// Apply writes the plan into the drop-in directory.
//
// Nothing is written to postgresql.conf except, when necessary, the include_dir
// directive that makes the drop-in directory readable at all.
func Apply(plan Plan, layout Layout, useSudo bool) (Result, error) {
	var result Result
	if plan.IsEmpty() {
		return result, nil
	}

	config, err := os.ReadFile(layout.ConfigFile)
	if err != nil {
		return result, fmt.Errorf("reading %s: %w", layout.ConfigFile, err)
	}

	if err := mkdirAll(layout.ConfDir, useSudo); err != nil {
		return result, err
	}

	// shared_preload_libraries is cumulative, so the merged value has to
	// account for what postgresql.conf already sets and for what previous
	// pgbrew installs put in the shared drop-in.
	if plan.PreloadLibrary != "" {
		existing := currentPreloadLibraries(string(config), layout)
		merged, _ := MergePreload(existing, plan.PreloadLibrary)

		preloadPath := filepath.Join(layout.ConfDir, PreloadFileName)
		if err := writeFile(preloadPath, RenderPreloadFile(merged), useSudo); err != nil {
			return result, err
		}
		result.Written = append(result.Written, preloadPath)
		result.PreloadLibraries = merged
	}

	if len(plan.Settings) > 0 {
		dropInPath := filepath.Join(layout.ConfDir, DropInName(plan.Extension))
		if err := writeFile(dropInPath, RenderDropIn(plan.Extension, plan.Settings), useSudo); err != nil {
			return result, err
		}
		result.Written = append(result.Written, dropInPath)
	}

	// The drop-ins are useless unless postgresql.conf reads the directory.
	if !HasIncludeDir(string(config), ConfDirName) {
		if err := appendFile(layout.ConfigFile, IncludeDirLine(ConfDirName), useSudo); err != nil {
			return result, err
		}
		result.IncludeDirAdded = true
	}

	result.RestartRequired = plan.NeedsRestart()
	return result, nil
}

// currentPreloadLibraries reads the effective library list, combining
// postgresql.conf with any list pgbrew has already written.
func currentPreloadLibraries(config string, layout Layout) []string {
	var libs []string

	if value, ok := FindSetting(config, PreloadSetting); ok {
		libs = ParsePreloadList(value)
	}

	// Our drop-in is read after postgresql.conf, so it must carry the union of
	// both — otherwise re-writing it would drop everything added previously.
	preloadPath := filepath.Join(layout.ConfDir, PreloadFileName)
	if data, err := os.ReadFile(preloadPath); err == nil {
		if value, ok := FindSetting(string(data), PreloadSetting); ok {
			libs, _ = MergePreload(libs, ParsePreloadList(value)...)
		}
	}

	return libs
}

// Remove deletes an extension's drop-in and takes its library back out of the
// merged preload list. Used by uninstall, so a removed extension does not leave
// a dangling preload entry that prevents the server from starting.
func Remove(extension, preloadLibrary string, layout Layout, useSudo bool) (Result, error) {
	var result Result

	dropInPath := filepath.Join(layout.ConfDir, DropInName(extension))
	if _, err := os.Stat(dropInPath); err == nil {
		if err := removeFile(dropInPath, useSudo); err != nil {
			return result, err
		}
		result.Written = append(result.Written, dropInPath)
	}

	preloadPath := filepath.Join(layout.ConfDir, PreloadFileName)
	data, err := os.ReadFile(preloadPath)
	if err != nil {
		return result, nil // nothing preloaded by us
	}

	value, ok := FindSetting(string(data), PreloadSetting)
	if !ok {
		return result, nil
	}

	name := preloadLibrary
	if name == "" {
		name = extension
	}
	kept, changed := RemoveFromPreload(ParsePreloadList(value), name)
	if !changed {
		return result, nil
	}

	if len(kept) == 0 {
		if err := removeFile(preloadPath, useSudo); err != nil {
			return result, err
		}
	} else if err := writeFile(preloadPath, RenderPreloadFile(kept), useSudo); err != nil {
		return result, err
	}

	result.Written = append(result.Written, preloadPath)
	result.PreloadLibraries = kept
	result.RestartRequired = true
	return result, nil
}

func mkdirAll(dir string, useSudo bool) error {
	if !useSudo {
		return os.MkdirAll(dir, 0o755)
	}
	return run("sudo", "mkdir", "-p", dir)
}

func writeFile(path, content string, useSudo bool) error {
	if !useSudo {
		return os.WriteFile(path, []byte(content), 0o644)
	}

	tmp, err := os.CreateTemp("", "pgbrew-conf-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return run("sudo", "install", "-m", "0644", tmp.Name(), path)
}

func appendFile(path, content string, useSudo bool) error {
	if !useSudo {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.WriteString(content)
		return err
	}
	// tee -a keeps the existing file's ownership and mode.
	cmd := exec.Command("sudo", "tee", "-a", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func removeFile(path string, useSudo bool) error {
	if !useSudo {
		return os.Remove(path)
	}
	return run("sudo", "rm", "-f", path)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
