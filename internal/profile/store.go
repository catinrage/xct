package profile

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xct/internal/domain"
)

type Store struct {
	BaseDir     string
	ProfilesDir string
	XrayBinFile string
}

func NewStore(baseDir string) Store {
	if baseDir == "" {
		baseDir = domain.DefaultBaseDir
	}
	return Store{
		BaseDir:     baseDir,
		ProfilesDir: filepath.Join(baseDir, "profiles"),
		XrayBinFile: filepath.Join(baseDir, "xray_bin"),
	}
}

func (s Store) Ensure() error {
	if err := os.MkdirAll(s.ProfilesDir, 0o700); err != nil {
		return err
	}
	return os.Chmod(s.BaseDir, 0o700)
}

func (s Store) ProfileDir(name string) string {
	return filepath.Join(s.ProfilesDir, name)
}

func (s Store) EnvPath(name string) string {
	return filepath.Join(s.ProfileDir(name), "profile.env")
}

func (s Store) SaveXrayBin(path string) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	return os.WriteFile(s.XrayBinFile, []byte(path+"\n"), 0o600)
}

func (s Store) LoadXrayBin() string {
	content, err := os.ReadFile(s.XrayBinFile)
	if err != nil {
		return domain.DefaultXrayBin
	}
	value := strings.TrimSpace(string(content))
	if value == "" {
		return domain.DefaultXrayBin
	}
	return value
}

func (s Store) Save(p domain.Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	dir := s.ProfileDir(p.Profile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(s.EnvPath(p.Profile), []byte(EncodeEnv(p)), 0o600); err != nil {
		return err
	}
	return nil
}

func (s Store) Load(name string) (domain.Profile, error) {
	path := s.EnvPath(name)
	file, err := os.Open(path)
	if err != nil {
		return domain.Profile{}, err
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
		values[key] = unquoteShell(value)
	}
	if err := scanner.Err(); err != nil {
		return domain.Profile{}, err
	}
	p := DecodeEnv(values)
	return p, nil
}

func (s Store) List() ([]domain.Profile, error) {
	entries, err := os.ReadDir(s.ProfilesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []domain.Profile
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p, err := s.Load(entry.Name())
		if err == nil {
			profiles = append(profiles, p)
		}
	}
	return profiles, nil
}

func EncodeEnv(p domain.Profile) string {
	fields := []struct {
		key string
		val string
	}{
		{"PROFILE", p.Profile},
		{"TYPE", string(p.Type)},
		{"DOMAIN", p.Domain},
		{"CDN_PORT", itoa(p.CDNPort)},
		{"WS_PATH", p.WSPath},
		{"SSL_CRT", p.SSLCrt},
		{"SSL_KEY", p.SSLKey},
		{"XRAY_BIN", p.XrayBin},
		{"REMOTE_XRAY_BIN", p.RemoteXrayBin},
		{"BACKEND_PORT", itoa(p.BackendPort)},
		{"REMOTE_UUID", p.RemoteUUID},
		{"LOCAL_UUID", p.LocalUUID},
		{"IRAN_SERVICE", p.IranService},
		{"IRAN_XRAY_CONFIG", p.IranXrayConfig},
		{"IRAN_NGINX_SITE", p.IranNginxSite},
		{"OUTER_SERVICE", p.OuterService},
		{"OUTER_XRAY_CONFIG", p.OuterXrayConfig},
		{"IRAN_SOCKS_LISTEN", p.IranSocksListen},
		{"IRAN_SOCKS_PORT", itoa(p.IranSocksPort)},
		{"IRAN_SOCKS_USER", p.IranSocksUser},
		{"IRAN_SOCKS_PASS", p.IranSocksPass},
		{"IRAN_LOCAL_VLESS_LISTEN", p.IranLocalVLESSListen},
		{"IRAN_LOCAL_VLESS_PORT", itoa(p.IranLocalVLESSPort)},
		{"IRAN_LOCAL_VLESS_UUID", p.IranLocalVLESSUUID},
		{"OUTER_LOCAL_VLESS_LISTEN", p.OuterLocalVLESSListen},
		{"OUTER_LOCAL_VLESS_PORT", itoa(p.OuterLocalVLESSPort)},
		{"OUTER_LOCAL_VLESS_UUID", p.OuterLocalVLESSUUID},
		{"OUTER_SOCKS_LISTEN", p.OuterSocksListen},
		{"OUTER_SOCKS_PORT", itoa(p.OuterSocksPort)},
		{"OUTER_SOCKS_USER", p.OuterSocksUser},
		{"OUTER_SOCKS_PASS", p.OuterSocksPass},
		{"SSH_HOST", p.SSHHost},
		{"SSH_PORT", itoa(p.SSHPort)},
		{"SSH_USER", p.SSHUser},
		{"SSH_AUTH", string(p.SSHAuth)},
		{"SSH_KEY", p.SSHKey},
		{"SSH_SOCKS_ENABLED", yesNo(p.SSHSOCKSEnabled)},
		{"SSH_SOCKS_HOST", p.SSHSOCKSHost},
		{"SSH_SOCKS_PORT", itoa(p.SSHSOCKSPort)},
		{"SSH_SOCKS_USER", p.SSHSOCKSUser},
		{"SSH_SOCKS_PASS", p.SSHSOCKSPass},
		{"REMOTE_ROOT_MODE", p.RemoteRootMode},
		{"DEPLOY_STATUS", p.DeployStatus},
		{"FAILED_STEP", p.FailedStep},
	}
	var b strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&b, "%s=%s\n", field.key, shellQuote(field.val))
	}
	return b.String()
}

func DecodeEnv(values map[string]string) domain.Profile {
	return domain.Profile{
		Profile:               values["PROFILE"],
		Type:                  domain.TunnelType(values["TYPE"]),
		Domain:                values["DOMAIN"],
		CDNPort:               atoi(values["CDN_PORT"]),
		WSPath:                values["WS_PATH"],
		SSLCrt:                values["SSL_CRT"],
		SSLKey:                values["SSL_KEY"],
		XrayBin:               values["XRAY_BIN"],
		RemoteXrayBin:         values["REMOTE_XRAY_BIN"],
		BackendPort:           atoi(values["BACKEND_PORT"]),
		RemoteUUID:            values["REMOTE_UUID"],
		LocalUUID:             values["LOCAL_UUID"],
		IranService:           values["IRAN_SERVICE"],
		IranXrayConfig:        values["IRAN_XRAY_CONFIG"],
		IranNginxSite:         values["IRAN_NGINX_SITE"],
		OuterService:          values["OUTER_SERVICE"],
		OuterXrayConfig:       values["OUTER_XRAY_CONFIG"],
		IranSocksListen:       values["IRAN_SOCKS_LISTEN"],
		IranSocksPort:         atoi(values["IRAN_SOCKS_PORT"]),
		IranSocksUser:         values["IRAN_SOCKS_USER"],
		IranSocksPass:         values["IRAN_SOCKS_PASS"],
		IranLocalVLESSListen:  values["IRAN_LOCAL_VLESS_LISTEN"],
		IranLocalVLESSPort:    atoi(values["IRAN_LOCAL_VLESS_PORT"]),
		IranLocalVLESSUUID:    values["IRAN_LOCAL_VLESS_UUID"],
		OuterLocalVLESSListen: values["OUTER_LOCAL_VLESS_LISTEN"],
		OuterLocalVLESSPort:   atoi(values["OUTER_LOCAL_VLESS_PORT"]),
		OuterLocalVLESSUUID:   values["OUTER_LOCAL_VLESS_UUID"],
		OuterSocksListen:      values["OUTER_SOCKS_LISTEN"],
		OuterSocksPort:        atoi(values["OUTER_SOCKS_PORT"]),
		OuterSocksUser:        values["OUTER_SOCKS_USER"],
		OuterSocksPass:        values["OUTER_SOCKS_PASS"],
		SSHHost:               values["SSH_HOST"],
		SSHPort:               atoi(values["SSH_PORT"]),
		SSHUser:               values["SSH_USER"],
		SSHAuth:               domain.SSHAuth(values["SSH_AUTH"]),
		SSHKey:                values["SSH_KEY"],
		SSHSOCKSEnabled:       values["SSH_SOCKS_ENABLED"] == "yes",
		SSHSOCKSHost:          values["SSH_SOCKS_HOST"],
		SSHSOCKSPort:          atoi(values["SSH_SOCKS_PORT"]),
		SSHSOCKSUser:          values["SSH_SOCKS_USER"],
		SSHSOCKSPass:          values["SSH_SOCKS_PASS"],
		RemoteRootMode:        values["REMOTE_ROOT_MODE"],
		DeployStatus:          values["DEPLOY_STATUS"],
		FailedStep:            values["FAILED_STEP"],
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func unquoteShell(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		body := strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
		return strings.ReplaceAll(body, `'\''`, "'")
	}
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			return unquoted
		}
	}
	return value
}

func atoi(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func itoa(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
