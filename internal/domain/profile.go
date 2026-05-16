package domain

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
)

const (
	DefaultBaseDir       = "/etc/xray-cdn-tunnel-controller"
	DefaultXrayBin       = "/usr/local/x-ui/bin/xray-linux-amd64"
	DefaultRemoteXrayBin = "/usr/local/bin/xray"
	ChabokanURL          = "https://chabokan.net/ip/"
)

type TunnelType string

const (
	Reverse TunnelType = "reverse"
	Direct  TunnelType = "direct"
)

type SSHAuth string

const (
	SSHKey      SSHAuth = "key"
	SSHPassword SSHAuth = "password"
)

type Profile struct {
	Profile string
	Type    TunnelType

	Domain  string
	CDNPort int
	WSPath  string
	SSLCrt  string
	SSLKey  string

	XrayBin       string
	RemoteXrayBin string
	BackendPort   int
	RemoteUUID    string
	LocalUUID     string

	IranService     string
	IranXrayConfig  string
	IranNginxSite   string
	OuterService    string
	OuterXrayConfig string

	IranSocksListen      string
	IranSocksPort        int
	IranSocksUser        string
	IranSocksPass        string
	IranLocalVLESSListen string
	IranLocalVLESSPort   int
	IranLocalVLESSUUID   string

	OuterLocalVLESSListen string
	OuterLocalVLESSPort   int
	OuterLocalVLESSUUID   string
	OuterSocksListen      string
	OuterSocksPort        int
	OuterSocksUser        string
	OuterSocksPass        string

	SSHHost         string
	SSHPort         int
	SSHUser         string
	SSHAuth         SSHAuth
	SSHKey          string
	SSHSOCKSEnabled bool
	SSHSOCKSHost    string
	SSHSOCKSPort    int
	SSHSOCKSUser    string
	SSHSOCKSPass    string
	RemoteRootMode  string

	DeployStatus string
	FailedStep   string
}

var (
	profileNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	wsPathRE      = regexp.MustCompile(`^/[A-Za-z0-9._~/-]+$`)
)

func ValidateProfileName(value string) error {
	if !profileNameRE.MatchString(value) {
		return errors.New("profile name may only contain A-Z, a-z, 0-9, underscore, and dash")
	}
	return nil
}

func ValidatePath(value string) error {
	if !wsPathRE.MatchString(value) {
		return errors.New("transport path must start with / and contain simple URL path characters")
	}
	return nil
}

func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535: %d", port)
	}
	return nil
}

func (p *Profile) FillDerivedPaths(baseDir string) {
	if p.XrayBin == "" {
		p.XrayBin = DefaultXrayBin
	}
	if p.RemoteXrayBin == "" {
		p.RemoteXrayBin = DefaultRemoteXrayBin
	}
	if p.SSHPort == 0 {
		p.SSHPort = 22
	}
	if p.SSHUser == "" {
		p.SSHUser = "root"
	}
	if p.SSHAuth == "" {
		p.SSHAuth = SSHKey
	}
	if p.RemoteRootMode == "" {
		p.RemoteRootMode = "root"
	}

	switch p.Type {
	case Reverse:
		p.IranXrayConfig = filepath.Join(baseDir, p.Profile+"-iran-reverse.json")
		p.IranService = "xct-reverse-" + p.Profile + "-iran.service"
		p.IranNginxSite = "/etc/nginx/sites-available/xct-" + p.Profile + "-reverse.conf"
		p.OuterXrayConfig = filepath.Join(baseDir, p.Profile+"-outer-reverse.json")
		p.OuterService = "xct-reverse-" + p.Profile + "-outer.service"
	case Direct:
		p.IranXrayConfig = filepath.Join(baseDir, p.Profile+"-iran-direct.json")
		p.IranService = "xct-direct-" + p.Profile + "-iran.service"
		p.IranNginxSite = "/etc/nginx/sites-available/xct-" + p.Profile + "-direct.conf"
		p.OuterXrayConfig = filepath.Join(baseDir, p.Profile+"-outer-direct.json")
		p.OuterService = "xct-direct-" + p.Profile + "-outer.service"
	}
}

func (p Profile) Validate() error {
	if err := ValidateProfileName(p.Profile); err != nil {
		return err
	}
	if p.Type != Reverse && p.Type != Direct {
		return fmt.Errorf("unsupported tunnel type: %s", p.Type)
	}
	if p.Domain == "" {
		return errors.New("domain is required")
	}
	if err := ValidatePort(p.CDNPort); err != nil {
		return err
	}
	if err := ValidatePath(p.WSPath); err != nil {
		return err
	}
	if err := ValidatePort(p.BackendPort); err != nil {
		return err
	}
	if p.RemoteUUID == "" {
		return errors.New("remote UUID is required")
	}
	if p.SSHHost == "" {
		return errors.New("outer SSH host is required")
	}
	if err := ValidatePort(p.SSHPort); err != nil {
		return fmt.Errorf("outer SSH %w", err)
	}
	if p.SSHAuth != SSHKey && p.SSHAuth != SSHPassword {
		return fmt.Errorf("SSH auth must be %q or %q", SSHKey, SSHPassword)
	}
	if p.Type == Reverse {
		if err := ValidatePort(p.IranSocksPort); err != nil {
			return err
		}
		if err := ValidatePort(p.IranLocalVLESSPort); err != nil {
			return err
		}
		if p.IranLocalVLESSUUID == "" {
			return errors.New("Iran local VLESS UUID is required for reverse profiles")
		}
	}
	if p.Type == Direct {
		if err := ValidatePort(p.OuterLocalVLESSPort); err != nil {
			return err
		}
		if err := ValidatePort(p.OuterSocksPort); err != nil {
			return err
		}
		if p.OuterLocalVLESSUUID == "" {
			return errors.New("outer local VLESS UUID is required for direct profiles")
		}
	}
	return nil
}
