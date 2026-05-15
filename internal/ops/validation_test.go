package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xct/internal/domain"
)

func TestValidateExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "xray")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutable(path); err == nil {
		t.Fatalf("expected non-executable file to fail")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutable(path); err != nil {
		t.Fatal(err)
	}
}

func TestUniquePositivePorts(t *testing.T) {
	got := uniquePositivePorts([]int{0, 443, 443, 2087})
	if len(got) != 2 || got[0] != 443 || got[1] != 2087 {
		t.Fatalf("unexpected ports: %#v", got)
	}
}

func TestRemotePortCheckCommandSkipsActiveServiceOnResume(t *testing.T) {
	p := sampleDirectProfileForValidation()
	cmd := remotePortCheckCommand(p, true)
	if !strings.Contains(cmd, "systemctl is-active --quiet 'xct-direct-demo-outer.service'") {
		t.Fatalf("expected active service guard in resume command:\n%s", cmd)
	}
	if !strings.Contains(cmd, "20151") || !strings.Contains(cmd, "20152") {
		t.Fatalf("expected outer ports in command:\n%s", cmd)
	}
}

func sampleDirectProfileForValidation() domain.Profile {
	p := domain.Profile{
		Profile:               "demo",
		Type:                  domain.Direct,
		Domain:                "example.com",
		CDNPort:               2083,
		WSPath:                "/xct-direct-demo",
		BackendPort:           18192,
		RemoteUUID:            "remote",
		OuterLocalVLESSListen: "127.0.0.1",
		OuterLocalVLESSPort:   20151,
		OuterLocalVLESSUUID:   "local",
		OuterSocksListen:      "127.0.0.1",
		OuterSocksPort:        20152,
		OuterSocksUser:        "rain",
		OuterSocksPass:        "2013",
		SSHHost:               "127.0.0.1",
		SSHPort:               22,
		SSHUser:               "root",
		SSHAuth:               domain.SSHPassword,
		RemoteRootMode:        "root",
	}
	p.FillDerivedPaths("/etc/xray-cdn-tunnel-controller")
	return p
}
