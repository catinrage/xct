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
		"command -v nginx",
		"curl -fsSL https://github.com/XTLS/Xray-install/raw/main/install-release.sh",
		"bash /tmp/xct-xray-install.sh install",
		"test -x '/usr/local/bin/xray'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("bootstrap command missing %q:\n%s", want, cmd)
		}
	}
}
