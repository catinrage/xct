package update

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectCandidateIncludesPrerelease(t *testing.T) {
	releases := []Release{
		{
			Name:       "0.0.9-abc",
			TagName:    "build-9-abc",
			Prerelease: true,
			Assets: []Asset{
				{Name: "xct_0.0.9-abc_linux_amd64.tar.gz", BrowserDownloadURL: "https://example/archive"},
				{Name: "xct_0.0.9-abc_linux_amd64.tar.gz.sha256", BrowserDownloadURL: "https://example/sum"},
			},
		},
	}
	got, err := selectCandidate(releases, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "0.0.9-abc" || got.Checksum.BrowserDownloadURL == "" {
		t.Fatalf("unexpected candidate: %#v", got)
	}
}

func TestSelectCandidateSkipsPrereleaseWhenDisabled(t *testing.T) {
	_, err := selectCandidate([]Release{{Name: "pre", Prerelease: true, Assets: []Asset{{Name: "xct_pre_linux_amd64.tar.gz", BrowserDownloadURL: "x"}}}}, false)
	if err == nil {
		t.Fatalf("expected no candidate")
	}
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "xct.tar.gz")
	if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("archive"))
	checksum := filepath.Join(dir, "xct.tar.gz.sha256")
	if err := os.WriteFile(checksum, []byte(fmt.Sprintf("%x  xct.tar.gz\n", sum)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(archive, checksum); err != nil {
		t.Fatal(err)
	}
}

func TestExtractArchiveRejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "../bad", Mode: 0o644, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, filepath.Join(dir, "out")); err == nil {
		t.Fatalf("expected unsafe path to be rejected")
	}
}
