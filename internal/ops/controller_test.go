package ops

import (
	"strings"
	"testing"
)

func TestRemoteBootstrapCommandInstallsOuterPrereqs(t *testing.T) {
	cmd := remoteBootstrapCommand("/usr/local/bin/xray")
	for _, want := range []string{
		"apt-get install -y",
		"dnf install -y",
		"yum install -y",
		"apt-get install unzip -y",
		`bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install --without-geodata`,
		"test -x '/usr/local/bin/xray'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("bootstrap command missing %q:\n%s", want, cmd)
		}
	}
	for _, unwanted := range []string{"command -v nginx", "install_pkg nginx", "enable --now nginx"} {
		if strings.Contains(cmd, unwanted) {
			t.Fatalf("bootstrap command should not install outer nginx; found %q:\n%s", unwanted, cmd)
		}
	}
}
