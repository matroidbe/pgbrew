package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pgxArchive(t *testing.T, binary string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pgx", Mode: 0755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(binary))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeReleases serves a release laid out the way the release workflow
// publishes it, and points releasesURL at it for the test.
func fakeReleases(t *testing.T, tag string, archive []byte, sums string) {
	t.Helper()
	asset := releaseAsset(tag, "linux", "arm64")
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/releases/download/"+tag+"/pgx-"+tag+"-checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sums)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	old := releasesURL
	releasesURL = srv.URL + "/releases"
	t.Cleanup(func() { releasesURL = old })
}

func TestLatestReleaseReadsTheTagFromTheRedirect(t *testing.T) {
	fakeReleases(t, "v0.2.0", nil, "")

	tag, err := latestRelease(http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.2.0" {
		t.Errorf("tag = %q, want v0.2.0", tag)
	}
}

func TestDownloadReleaseReturnsTheVerifiedBinary(t *testing.T) {
	archive := pgxArchive(t, "#!new pgx")
	sums := fmt.Sprintf("%x  %s\n%x  pgx-v0.2.0-darwin-arm64.tar.gz\n",
		sha256.Sum256(archive), releaseAsset("v0.2.0", "linux", "arm64"), sha256.Sum256(nil))
	fakeReleases(t, "v0.2.0", archive, sums)

	binary, err := downloadRelease(http.DefaultClient, "v0.2.0", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if string(binary) != "#!new pgx" {
		t.Errorf("binary = %q, want the archived pgx", binary)
	}
}

func TestDownloadReleaseRejectsAChecksumMismatch(t *testing.T) {
	archive := pgxArchive(t, "#!tampered pgx")
	sums := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("something else")), releaseAsset("v0.2.0", "linux", "arm64"))
	fakeReleases(t, "v0.2.0", archive, sums)

	_, err := downloadRelease(http.DefaultClient, "v0.2.0", "linux", "arm64")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
}

func TestDownloadReleaseRejectsAnAssetMissingFromTheChecksums(t *testing.T) {
	archive := pgxArchive(t, "#!new pgx")
	fakeReleases(t, "v0.2.0", archive, "abc123  pgx-v0.2.0-darwin-arm64.tar.gz\n")

	_, err := downloadRelease(http.DefaultClient, "v0.2.0", "linux", "arm64")
	if err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("err = %v, want the asset reported as unlisted", err)
	}
}

func TestDownloadReleaseReportsAPlatformWithoutABinary(t *testing.T) {
	fakeReleases(t, "v0.2.0", nil, "")

	_, err := downloadRelease(http.DefaultClient, "v0.2.0", "windows", "amd64")
	if err == nil || !strings.Contains(err.Error(), "no prebuilt pgx v0.2.0 for windows-amd64") {
		t.Fatalf("err = %v, want no prebuilt binary for the platform", err)
	}
}

func TestReplaceExecutableSwapsTheFileInPlace(t *testing.T) {
	target := filepath.Join(t.TempDir(), "pgx")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(target, []byte("new")); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Errorf("target = %q, want new", got)
	}
	if info, _ := os.Stat(target); info.Mode().Perm()&0100 == 0 {
		t.Errorf("target mode = %v, want executable", info.Mode())
	}
	if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind")
	}
}
