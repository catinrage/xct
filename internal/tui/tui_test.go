package tui

import (
	"testing"

	"xct/internal/build"
	"xct/internal/domain"
	"xct/internal/ops"
)

func TestBuildProfileUsesCheckboxValuesAndRemoteXrayDefault(t *testing.T) {
	values := map[string]string{
		"profile":            "direct-demo",
		"domain":             "sky.example.com",
		"ssl_crt":            "/etc/ssl/cert.pem",
		"ssl_key":            "/etc/ssl/key.pem",
		"cdn_port":           "2083",
		"ws_path":            "/xct-direct-demo",
		"backend_port":       "18192",
		"ssh_host":           "outer.example.com",
		"ssh_port":           "22",
		"ssh_user":           "root",
		"ssh_auth":           "key",
		"ssh_socks_enabled":  "yes",
		"ssh_socks_port":     "20130",
		"remote_root_mode":   "root",
		"apply_tuning":       "no",
		"outer_vless_listen": "127.0.0.1",
		"outer_vless_port":   "20151",
		"outer_socks_listen": "127.0.0.1",
		"outer_socks_port":   "20152",
		"outer_socks_user":   "rain",
		"outer_socks_pass":   "2013",
	}

	p, applyTuning, err := buildProfile(values, actionCreateDirect)
	if err != nil {
		t.Fatal(err)
	}
	if applyTuning {
		t.Fatalf("expected apply tuning checkbox to be false")
	}
	if !p.SSHSOCKSEnabled {
		t.Fatalf("expected SSH SOCKS checkbox to be true")
	}
	if p.RemoteXrayBin != domain.DefaultRemoteXrayBin {
		t.Fatalf("unexpected remote xray default: %s", p.RemoteXrayBin)
	}
}

func TestVisibleFieldRangeTracksFocusedField(t *testing.T) {
	m := NewModel(ops.New(t.TempDir()), build.Info{Version: "test"})
	fields := commonFields("reverse")
	m.fields = fields
	m.inputs = nil
	m.height = 18
	m.focus = len(fields) - 1

	start, end := m.visibleFieldRange()
	if end != len(fields) {
		t.Fatalf("expected range to include final focused field, got %d-%d of %d", start, end, len(fields))
	}
	if start == 0 {
		t.Fatalf("expected long form to scroll away from top")
	}
}

func TestSettingsFromValues(t *testing.T) {
	cfg := settingsFromValues(map[string]string{
		"update_socks_enabled": "yes",
		"update_socks_host":    "127.0.0.1",
		"update_socks_port":    "1080",
		"update_socks_user":    "u",
		"update_socks_pass":    "p",
		"include_prerelease":   "no",
	})
	if !cfg.UpdateSOCKSEnabled || cfg.UpdateSOCKSHost != "127.0.0.1" || cfg.UpdateSOCKSPort != 1080 {
		t.Fatalf("unexpected settings: %#v", cfg)
	}
	if cfg.IncludePrerelease {
		t.Fatalf("expected prerelease setting to be false")
	}
}

func TestCreateFieldsHaveHelpText(t *testing.T) {
	fields := append(reverseFields(), directFields()...)
	for _, f := range fields {
		if fieldHelp(f.key) == "No help is available for this field yet." {
			t.Fatalf("missing help text for field %q", f.key)
		}
	}
}
