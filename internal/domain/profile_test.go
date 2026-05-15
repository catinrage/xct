package domain

import "testing"

func TestValidateProfileName(t *testing.T) {
	valid := []string{"demo", "direct-de01", "reverse_01", "A9"}
	for _, value := range valid {
		if err := ValidateProfileName(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}

	invalid := []string{"", "../x", "bad name", "bad.name"}
	for _, value := range invalid {
		if err := ValidateProfileName(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestFillDerivedPaths(t *testing.T) {
	p := Profile{Profile: "demo", Type: Reverse}
	p.FillDerivedPaths("/tmp/xct")

	if p.IranService != "xct-reverse-demo-iran.service" {
		t.Fatalf("unexpected Iran service: %s", p.IranService)
	}
	if p.OuterXrayConfig != "/tmp/xct/demo-outer-reverse.json" {
		t.Fatalf("unexpected outer config: %s", p.OuterXrayConfig)
	}
	if p.RemoteRootMode != "root" {
		t.Fatalf("expected root remote mode default")
	}
	if p.RemoteXrayBin != DefaultRemoteXrayBin {
		t.Fatalf("expected install-managed remote xray default, got %s", p.RemoteXrayBin)
	}
}
