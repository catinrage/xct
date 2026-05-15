package settings

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xct/internal/domain"
)

type Settings struct {
	UpdateRepo         string
	IncludePrerelease  bool
	UpdateSOCKSEnabled bool
	UpdateSOCKSHost    string
	UpdateSOCKSPort    int
	UpdateSOCKSUser    string
	UpdateSOCKSPass    string
}

type Store struct {
	BaseDir string
	Path    string
}

func NewStore(baseDir string) Store {
	if baseDir == "" {
		baseDir = domain.DefaultBaseDir
	}
	return Store{
		BaseDir: baseDir,
		Path:    filepath.Join(baseDir, "settings.env"),
	}
}

func Default() Settings {
	return Settings{
		UpdateRepo:        "catinrage/xct",
		IncludePrerelease: true,
		UpdateSOCKSPort:   20130,
	}
}

func (s Store) Load() (Settings, error) {
	cfg := Default()
	file, err := os.Open(s.Path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = unquote(value)
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}

	if values["UPDATE_REPO"] != "" {
		cfg.UpdateRepo = values["UPDATE_REPO"]
	}
	if values["INCLUDE_PRERELEASE"] != "" {
		cfg.IncludePrerelease = yes(values["INCLUDE_PRERELEASE"])
	}
	cfg.UpdateSOCKSEnabled = yes(values["UPDATE_SOCKS_ENABLED"])
	if values["UPDATE_SOCKS_HOST"] != "" {
		cfg.UpdateSOCKSHost = values["UPDATE_SOCKS_HOST"]
	}
	if values["UPDATE_SOCKS_PORT"] != "" {
		cfg.UpdateSOCKSPort, _ = strconv.Atoi(values["UPDATE_SOCKS_PORT"])
	}
	cfg.UpdateSOCKSUser = values["UPDATE_SOCKS_USER"]
	cfg.UpdateSOCKSPass = values["UPDATE_SOCKS_PASS"]
	return cfg, nil
}

func (s Store) Save(cfg Settings) error {
	if cfg.UpdateRepo == "" {
		cfg.UpdateRepo = Default().UpdateRepo
	}
	if cfg.UpdateSOCKSPort == 0 {
		cfg.UpdateSOCKSPort = Default().UpdateSOCKSPort
	}
	if err := os.MkdirAll(s.BaseDir, 0o700); err != nil {
		return err
	}
	data := strings.Builder{}
	data.WriteString("UPDATE_REPO=" + quote(cfg.UpdateRepo) + "\n")
	data.WriteString("INCLUDE_PRERELEASE=" + quote(yesNo(cfg.IncludePrerelease)) + "\n")
	data.WriteString("UPDATE_SOCKS_ENABLED=" + quote(yesNo(cfg.UpdateSOCKSEnabled)) + "\n")
	data.WriteString("UPDATE_SOCKS_HOST=" + quote(cfg.UpdateSOCKSHost) + "\n")
	data.WriteString("UPDATE_SOCKS_PORT=" + quote(strconv.Itoa(cfg.UpdateSOCKSPort)) + "\n")
	data.WriteString("UPDATE_SOCKS_USER=" + quote(cfg.UpdateSOCKSUser) + "\n")
	data.WriteString("UPDATE_SOCKS_PASS=" + quote(cfg.UpdateSOCKSPass) + "\n")
	return os.WriteFile(s.Path, []byte(data.String()), 0o600)
}

func yes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "y", "yes":
		return true
	default:
		return false
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		body := strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
		return strings.ReplaceAll(body, `'\''`, "'")
	}
	return value
}
