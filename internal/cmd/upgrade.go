package cmd

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// releasesURL is the GitHub releases page of pgbrew; tests point it at a fake.
var releasesURL = "https://github.com/matroidbe/pgbrew/releases"

var upgradeCmd = &cobra.Command{
	Use:   "upgrade [version]",
	Short: "Upgrade pgx to the latest version",
	Long: `Download the prebuilt pgx for this platform from the GitHub release,
verify its sha256 checksum, and replace the running binary.

Pass a tag (e.g. v0.2.0) to install that release instead of the latest.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUpgrade,
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	fmt.Println("Upgrading pgx...")

	client := &http.Client{Timeout: 2 * time.Minute}

	version := "latest"
	if len(args) == 1 {
		version = args[0]
	}
	if version == "latest" {
		latest, err := latestRelease(client)
		if err != nil {
			return err
		}
		version = latest
	}
	if version == Version {
		fmt.Printf("✓ pgx %s is already the installed version\n", Version)
		return nil
	}

	fmt.Printf("Downloading pgx %s for %s-%s...\n", version, runtime.GOOS, runtime.GOARCH)
	binary, err := downloadRelease(client, version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	// Find current executable path
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find current executable: %w", err)
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	fmt.Printf("Installing to %s...\n", currentExe)
	if err := replaceExecutable(currentExe, binary); err != nil {
		return err
	}

	fmt.Printf("✓ pgx upgraded to %s\n", version)
	return nil
}

// releaseAsset names the archive the release workflow publishes for a platform.
func releaseAsset(version, goos, goarch string) string {
	return fmt.Sprintf("pgx-%s-%s-%s.tar.gz", version, goos, goarch)
}

// latestRelease resolves the latest release tag from the redirect GitHub
// sends for /releases/latest, which avoids the rate-limited API.
func latestRelease(client *http.Client) (string, error) {
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := noRedirect.Head(releasesURL + "/latest")
	if err != nil {
		return "", fmt.Errorf("failed to resolve the latest release: %w", err)
	}
	resp.Body.Close()

	location := resp.Header.Get("Location")
	tag := location[strings.LastIndex(location, "/")+1:]
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("failed to resolve the latest release (redirected to %q)", location)
	}
	return tag, nil
}

// downloadRelease fetches the pgx archive for a platform, checks it against
// the release's checksum file and returns the pgx binary inside it.
func downloadRelease(client *http.Client, version, goos, goarch string) ([]byte, error) {
	asset := releaseAsset(version, goos, goarch)
	base := releasesURL + "/download/" + version

	archive, err := fetch(client, base+"/"+asset)
	if err != nil {
		return nil, fmt.Errorf("no prebuilt pgx %s for %s-%s: %w", version, goos, goarch, err)
	}
	sums, err := fetch(client, base+"/pgx-"+version+"-checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("release %s has no checksums, refusing an unverified binary: %w", version, err)
	}

	expected, err := checksumFor(sums, asset)
	if err != nil {
		return nil, err
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expected {
		return nil, fmt.Errorf("checksum mismatch for %s (expected %s, got %x)", asset, expected, actual)
	}

	return extractPgx(archive)
}

func fetch(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// checksumFor finds an asset's digest in sha256sum output.
func checksumFor(sums []byte, asset string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("%s is not listed in the release checksums", asset)
}

// extractPgx returns the pgx binary from a release archive.
func extractPgx(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("failed to read release archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("release archive has no pgx binary")
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read release archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "pgx" {
			return io.ReadAll(tr)
		}
	}
}

// replaceExecutable swaps in the new binary. It writes a sibling file and
// renames it over the target, which works while the old binary is running.
func replaceExecutable(target string, binary []byte) error {
	tmp := target + ".new"
	if err := os.WriteFile(tmp, binary, 0755); err != nil {
		return fmt.Errorf("failed to write new executable: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to install new executable: %w", err)
	}
	return nil
}
