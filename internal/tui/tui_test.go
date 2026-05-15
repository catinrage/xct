package tui

import (
	"testing"

	"xct/internal/build"
	"xct/internal/domain"
	"xct/internal/ops"
	"xct/internal/settings"
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
		"ssh_auth":           "password",
		"ssh_password":       "secret",
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
	if p.SSHAuth != domain.SSHPassword {
		t.Fatalf("expected password auth default in test data")
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

func TestReactiveCreateFieldsHideKeyForPasswordAuth(t *testing.T) {
	m := NewModel(ops.New(t.TempDir()), build.Info{Version: "test"})
	model, _ := m.startForm(actionCreateDirect, directFields())
	got := model.(Model)

	if hasField(got.fields, "ssh_key") {
		t.Fatalf("expected ssh_key to be hidden for password auth")
	}
	if !hasField(got.fields, "ssh_password") {
		t.Fatalf("expected ssh_password to be visible for password auth")
	}

	got.setFieldValueForTest("ssh_auth", "key")
	got.rebuildVisibleFields("ssh_auth")
	if !hasField(got.fields, "ssh_key") {
		t.Fatalf("expected ssh_key to be visible for key auth")
	}
	if hasField(got.fields, "ssh_password") {
		t.Fatalf("expected ssh_password to be hidden for key auth")
	}
}

func TestReactiveCreateFieldsHideManualSocksWhenUsingSettings(t *testing.T) {
	ctrl := ops.New(t.TempDir())
	m := NewModel(ctrl, build.Info{Version: "test"})
	if err := m.settingsStore.Save(settingsForTest()); err != nil {
		t.Fatal(err)
	}
	model, _ := m.startForm(actionCreateDirect, directFields())
	got := model.(Model)

	if !hasField(got.fields, "use_update_socks") {
		t.Fatalf("expected settings SOCKS reuse field to appear when settings SOCKS is configured")
	}
	if hasField(got.fields, "ssh_socks_host") {
		t.Fatalf("expected manual SSH SOCKS fields to be hidden until manual SSH SOCKS is enabled")
	}
	got.setFieldValueForTest("use_update_socks", "yes")
	got.rebuildVisibleFields("use_update_socks")
	if hasField(got.fields, "ssh_socks_enabled") || hasField(got.fields, "ssh_socks_host") {
		t.Fatalf("expected manual SSH SOCKS fields to hide when reusing settings SOCKS")
	}
}

func TestSettingsSocksFieldsAreReactive(t *testing.T) {
	m := NewModel(ops.New(t.TempDir()), build.Info{Version: "test"})
	model, _ := m.startForm(actionEditSettings, m.settingsFields())
	got := model.(Model)

	if hasField(got.fields, "update_socks_host") {
		t.Fatalf("expected update SOCKS details to hide while disabled")
	}
	got.setFieldValueForTest("update_socks_enabled", "yes")
	got.rebuildVisibleFields("update_socks_enabled")
	if !hasField(got.fields, "update_socks_host") || !hasField(got.fields, "update_socks_port") {
		t.Fatalf("expected update SOCKS details to show while enabled")
	}
	if hasField(got.fields, "update_socks_pass") {
		t.Fatalf("expected update SOCKS password to hide until username is set")
	}
	got.setFieldValueForTest("update_socks_user", "rain")
	got.rebuildVisibleFields("update_socks_user")
	if !hasField(got.fields, "update_socks_pass") {
		t.Fatalf("expected update SOCKS password to show when username is set")
	}
}

func hasField(fields []field, key string) bool {
	for _, f := range fields {
		if f.key == key {
			return true
		}
	}
	return false
}

func (m *Model) setFieldValueForTest(key, value string) {
	for i := range m.allFields {
		if m.allFields[i].key == key {
			m.allFields[i].value = value
		}
	}
	for i := range m.fields {
		if m.fields[i].key == key {
			m.fields[i].value = value
			if i < len(m.inputs) {
				m.inputs[i].SetValue(value)
			}
		}
	}
}

func settingsForTest() settings.Settings {
	cfg := settings.Default()
	cfg.UpdateSOCKSEnabled = true
	cfg.UpdateSOCKSHost = "127.0.0.1"
	cfg.UpdateSOCKSPort = 1080
	return cfg
}
