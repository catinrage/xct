package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/proxy"

	"xct/internal/build"
	"xct/internal/settings"
)

type Client struct {
	Settings settings.Settings
	Build    build.Info
}

var (
	currentExecutable = os.Executable
	resolveSymlinks   = filepath.EvalSymlinks
	execProcess       = syscall.Exec
)

type Release struct {
	Name        string  `json:"name"`
	TagName     string  `json:"tag_name"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Candidate struct {
	Version     string
	Tag         string
	PublishedAt string
	Archive     Asset
	Checksum    Asset
}

func (c Client) Check(ctx context.Context) (string, error) {
	candidate, err := c.latest(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Current version: %s\n", c.currentVersion())
	fmt.Fprintf(&b, "Latest release: %s (%s)\n", candidate.Version, candidate.PublishedAt)
	if c.currentVersion() == candidate.Version {
		b.WriteString("Already up to date.\n")
	} else {
		b.WriteString("Update available.\n")
	}
	if c.Settings.UpdateSOCKSEnabled {
		fmt.Fprintf(&b, "GitHub access: SOCKS5 %s:%d\n", c.Settings.UpdateSOCKSHost, c.Settings.UpdateSOCKSPort)
	}
	return b.String(), nil
}

func (c Client) Install(ctx context.Context, force bool) (string, error) {
	candidate, err := c.latest(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	current := c.currentVersion()
	fmt.Fprintf(&b, "Current version: %s\n", current)
	fmt.Fprintf(&b, "Latest release: %s (%s)\n", candidate.Version, candidate.PublishedAt)
	if !force && current == candidate.Version {
		b.WriteString("Already up to date.\n")
		return b.String(), nil
	}

	tmp, err := os.MkdirTemp("", "xct-update-*")
	if err != nil {
		return b.String(), err
	}
	defer os.RemoveAll(tmp)

	archivePath := filepath.Join(tmp, candidate.Archive.Name)
	fmt.Fprintf(&b, "Downloading %s\n", candidate.Archive.Name)
	if err := c.download(ctx, candidate.Archive.BrowserDownloadURL, archivePath); err != nil {
		return b.String(), err
	}

	if candidate.Checksum.BrowserDownloadURL != "" {
		checksumPath := filepath.Join(tmp, candidate.Checksum.Name)
		fmt.Fprintf(&b, "Downloading %s\n", candidate.Checksum.Name)
		if err := c.download(ctx, candidate.Checksum.BrowserDownloadURL, checksumPath); err != nil {
			return b.String(), err
		}
		if err := verifyChecksum(archivePath, checksumPath); err != nil {
			return b.String(), err
		}
		b.WriteString("Checksum verified.\n")
	} else {
		b.WriteString("Warning: no checksum asset found for this release.\n")
	}

	extractDir := filepath.Join(tmp, "extract")
	if err := extractArchive(archivePath, extractDir); err != nil {
		return b.String(), err
	}

	exe, err := os.Executable()
	if err != nil {
		return b.String(), err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return b.String(), err
	}
	installDir := filepath.Dir(exe)
	newBinary, err := findFile(extractDir, "xct-controller")
	if err != nil {
		return b.String(), err
	}
	if err := installFile(newBinary, exe, 0o755); err != nil {
		return b.String(), err
	}
	if readme, err := findFile(extractDir, "README.md"); err == nil {
		_ = installFile(readme, filepath.Join(installDir, "README.md"), 0o644)
	}
	fmt.Fprintf(&b, "Update complete: %s -> %s\n", current, candidate.Version)
	return b.String(), nil
}

func (c Client) InstallAndRestart(ctx context.Context, force bool) (string, error) {
	output, err := c.Install(ctx, force)
	if err != nil {
		return output, err
	}
	if !strings.Contains(output, "Update complete:") {
		return output, nil
	}
	output += "Restarting into updated binary...\n"
	return output, RestartCurrentProcess()
}

func RestartCurrentProcess() error {
	exe, err := currentExecutable()
	if err != nil {
		return err
	}
	exe, err = resolveSymlinks(exe)
	if err != nil {
		return err
	}
	return execProcess(exe, os.Args, os.Environ())
}

func (c Client) latest(ctx context.Context) (Candidate, error) {
	repo := c.Settings.UpdateRepo
	if repo == "" {
		repo = settings.Default().UpdateRepo
	}
	apiURL := "https://api.github.com/repos/" + repo + "/releases"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Candidate{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Candidate{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return Candidate{}, fmt.Errorf("GitHub releases request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return Candidate{}, err
	}
	return selectCandidate(releases, c.Settings.IncludePrerelease)
}

func (c Client) download(ctx context.Context, rawURL, output string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func (c Client) httpClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 30 * time.Second,
	}
	if c.Settings.UpdateSOCKSEnabled {
		address := fmt.Sprintf("%s:%d", c.Settings.UpdateSOCKSHost, c.Settings.UpdateSOCKSPort)
		var auth *proxy.Auth
		if c.Settings.UpdateSOCKSUser != "" {
			auth = &proxy.Auth{User: c.Settings.UpdateSOCKSUser, Password: c.Settings.UpdateSOCKSPass}
		}
		dialer, err := proxy.SOCKS5("tcp", address, auth, proxy.Direct)
		if err == nil {
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				type contextDialer interface {
					DialContext(context.Context, string, string) (net.Conn, error)
				}
				if d, ok := dialer.(contextDialer); ok {
					return d.DialContext(ctx, network, addr)
				}
				return dialer.Dial(network, addr)
			}
		}
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Minute}
}

func (c Client) currentVersion() string {
	if c.Build.Version == "" {
		return "dev"
	}
	return c.Build.Version
}

func selectCandidate(releases []Release, includePrerelease bool) (Candidate, error) {
	for _, release := range releases {
		if release.Draft {
			continue
		}
		if release.Prerelease && !includePrerelease {
			continue
		}
		var archive Asset
		var checksum Asset
		for _, asset := range release.Assets {
			if strings.HasPrefix(asset.Name, "xct_") && strings.HasSuffix(asset.Name, "_linux_amd64.tar.gz") {
				archive = asset
			}
			if strings.HasPrefix(asset.Name, "xct_") && strings.HasSuffix(asset.Name, "_linux_amd64.tar.gz.sha256") {
				checksum = asset
			}
		}
		if archive.BrowserDownloadURL == "" {
			continue
		}
		version := release.Name
		if version == "" {
			version = release.TagName
		}
		return Candidate{Version: version, Tag: release.TagName, PublishedAt: release.PublishedAt, Archive: archive, Checksum: checksum}, nil
	}
	return Candidate{}, errors.New("no suitable linux amd64 release asset found")
}

func verifyChecksum(archivePath, checksumPath string) error {
	content, err := os.ReadFile(checksumPath)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		return errors.New("empty checksum file")
	}
	expected := fields[0]
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(sum.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s got %s", expected, actual)
	}
	return nil
}

func extractArchive(archivePath, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(header.Name)
		if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe archive path: %s", header.Name)
		}
		target := filepath.Join(dest, clean)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("%s not found in release archive", name)
	}
	return found, nil
}

func installFile(src, dest string, mode os.FileMode) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	tmp := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp")
	output, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func ParseProxyURL(raw string) (settings.Settings, error) {
	cfg := settings.Default()
	if raw == "" {
		return cfg, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return cfg, err
	}
	if u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return cfg, fmt.Errorf("unsupported proxy scheme: %s", u.Scheme)
	}
	cfg.UpdateSOCKSEnabled = true
	cfg.UpdateSOCKSHost = u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	cfg.UpdateSOCKSPort = port
	if cfg.UpdateSOCKSPort == 0 {
		cfg.UpdateSOCKSPort = 1080
	}
	if u.User != nil {
		cfg.UpdateSOCKSUser = u.User.Username()
		cfg.UpdateSOCKSPass, _ = u.User.Password()
	}
	return cfg, nil
}
