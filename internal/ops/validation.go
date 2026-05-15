package ops

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"xct/internal/domain"
)

type ValidationError struct {
	Problems []string
}

func (e ValidationError) Error() string {
	return "validation failed:\n- " + strings.Join(e.Problems, "\n- ")
}

func (c Controller) ValidateCreate(ctx context.Context, p domain.Profile) error {
	p.FillDerivedPaths(c.Store.BaseDir)
	var problems []string

	if err := p.Validate(); err != nil {
		problems = append(problems, err.Error())
	}
	if err := validateExecutable(p.XrayBin); err != nil {
		problems = append(problems, err.Error())
	} else if _, err := c.Runner.Run(ctx, p.XrayBin, "uuid"); err != nil {
		problems = append(problems, fmt.Sprintf("Iran Xray binary did not run correctly at %s: %v", p.XrayBin, err))
	}
	if err := validateFile("SSL certificate", p.SSLCrt); err != nil {
		problems = append(problems, err.Error())
	}
	if err := validateFile("SSL key", p.SSLKey); err != nil {
		problems = append(problems, err.Error())
	}

	for _, port := range localPortsForProfile(p) {
		if err := validateTCPPortFree(port); err != nil {
			problems = append(problems, err.Error())
		}
	}

	if err := validateSSHReachability(p); err != nil {
		problems = append(problems, err.Error())
	}

	if len(problems) > 0 {
		return ValidationError{Problems: problems}
	}
	return nil
}

func validateExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("Iran Xray binary not found: %s", path)
	}
	if info.IsDir() {
		return fmt.Errorf("Iran Xray binary path is a directory: %s", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("Iran Xray binary is not executable: %s", path)
	}
	return nil
}

func validateFile(label, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s file not found: %s", label, path)
	}
	if info.IsDir() {
		return fmt.Errorf("%s path is a directory: %s", label, path)
	}
	return nil
}

func localPortsForProfile(p domain.Profile) []int {
	ports := []int{p.CDNPort, p.BackendPort}
	if p.Type == domain.Reverse {
		ports = append(ports, p.IranSocksPort, p.IranLocalVLESSPort)
	}
	return uniquePositivePorts(ports)
}

func uniquePositivePorts(ports []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, port := range ports {
		if port <= 0 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	return out
}

func validateTCPPortFree(port int) error {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("local Iran TCP port %d is not free: %v", port, err)
	}
	return listener.Close()
}

func validateSSHReachability(p domain.Profile) error {
	address := net.JoinHostPort(p.SSHHost, strconv.Itoa(p.SSHPort))
	if p.SSHSOCKSEnabled {
		if p.SSHSOCKSHost == "" || p.SSHSOCKSPort == 0 {
			return errors.New("SSH SOCKS proxy is enabled but host/port is empty")
		}
		auth := (*proxy.Auth)(nil)
		if p.SSHSOCKSUser != "" {
			auth = &proxy.Auth{User: p.SSHSOCKSUser, Password: p.SSHSOCKSPass}
		}
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(p.SSHSOCKSHost, strconv.Itoa(p.SSHSOCKSPort)), auth, timeoutDialer{timeout: 8 * time.Second})
		if err != nil {
			return fmt.Errorf("could not create SSH SOCKS dialer: %v", err)
		}
		conn, err := dialer.Dial("tcp", address)
		if err != nil {
			return fmt.Errorf("SSH SOCKS proxy could not reach outer SSH %s: %v", address, err)
		}
		_ = conn.Close()
		return nil
	}
	conn, err := net.DialTimeout("tcp", address, 8*time.Second)
	if err != nil {
		return fmt.Errorf("outer SSH %s is not reachable from Iran: %v", address, err)
	}
	_ = conn.Close()
	return nil
}

type timeoutDialer struct {
	timeout time.Duration
}

func (d timeoutDialer) Dial(network, address string) (net.Conn, error) {
	return net.DialTimeout(network, address, d.timeout)
}
