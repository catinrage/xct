package profile

import (
	"strings"
	"testing"

	"xct/internal/domain"
)

func TestStoreRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	p := domain.Profile{
		Profile:              "demo",
		Type:                 domain.Reverse,
		Domain:               "sky.example.com",
		CDNPort:              2087,
		WSPath:               "/xct-reverse-demo",
		SSLCrt:               "/etc/ssl/cert.pem",
		SSLKey:               "/etc/ssl/key.pem",
		XrayBin:              domain.DefaultXrayBin,
		RemoteXrayBin:        domain.DefaultXrayBin,
		BackendPort:          18191,
		RemoteUUID:           "remote-uuid",
		LocalUUID:            "local-uuid",
		IranSocksListen:      "127.0.0.1",
		IranSocksPort:        20141,
		IranSocksUser:        "rain",
		IranSocksPass:        "pa'ss",
		IranLocalVLESSListen: "127.0.0.1",
		IranLocalVLESSPort:   20142,
		IranLocalVLESSUUID:   "local-uuid",
		SSHHost:              "outer.example.com",
		SSHPort:              22,
		SSHUser:              "root",
		SSHAuth:              domain.SSHKey,
		RemoteRootMode:       "root",
	}
	p.FillDerivedPaths(store.BaseDir)

	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.IranSocksPass != "pa'ss" {
		t.Fatalf("shell quoted password did not round-trip: %q", got.IranSocksPass)
	}
	if got.IranService != p.IranService {
		t.Fatalf("service mismatch: %s != %s", got.IranService, p.IranService)
	}
}

func TestEncodeEnvIsShellCompatible(t *testing.T) {
	env := EncodeEnv(domain.Profile{Profile: "demo", Type: domain.Direct, SSHAuth: domain.SSHPassword})
	if !strings.Contains(env, "PROFILE='demo'\n") {
		t.Fatalf("expected shell-compatible profile assignment: %s", env)
	}
}
