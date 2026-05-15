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
		"command -v nginx",
		`bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install --without-geodata`,
		"test -x '/usr/local/bin/xray'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("bootstrap command missing %q:\n%s", want, cmd)
		}
	}
}
