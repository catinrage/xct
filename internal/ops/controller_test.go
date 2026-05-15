package ops

import (
	"context"
	"strings"
	"testing"

	"xct/internal/domain"
)

func TestRemoteBootstrapCommandInstallsOuterPrereqs(t *testing.T) {
	cmd := remoteBootstrapCommand("/usr/local/bin/xray")
	for _, want := range []string{
		"apt-get install -y",
		"dnf install -y",
		"yum install -y",
		"apt-get install -y curl unzip ca-certificates",
		"update-ca-certificates",
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

func TestRemoteCommandPreservesMultilineScript(t *testing.T) {
	runner := &capturingRunner{}
	ctrl := New(t.TempDir())
	ctrl.Runner = runner
	profile := domain.Profile{
		SSHHost:        "outer.example.com",
		SSHPort:        22,
		SSHUser:        "root",
		SSHAuth:        domain.SSHKey,
		RemoteRootMode: "root",
	}

	script := "set -e\ninstall_pkg() {\n  apt-get install -y \"$@\"\n}\n"
	if _, err := ctrl.RemoteCommand(context.Background(), profile, script); err != nil {
		t.Fatal(err)
	}
	if runner.name != "ssh" {
		t.Fatalf("expected ssh runner, got %q", runner.name)
	}
	remote := runner.args[len(runner.args)-1]
	if strings.Contains(remote, `\n`) {
		t.Fatalf("remote command should contain real newlines, not escaped newlines:\n%s", remote)
	}
	if !strings.Contains(remote, "set -e\ninstall_pkg()") {
		t.Fatalf("remote command lost multiline script:\n%s", remote)
	}
	if !strings.Contains(remote, `apt-get install -y "$@"`) {
		t.Fatalf("remote command lost shell variable quoting:\n%s", remote)
	}
}

type capturingRunner struct {
	name string
	args []string
}

func (r *capturingRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return "", nil
}

func (r *capturingRunner) RunInput(ctx context.Context, input []byte, name string, args ...string) (string, error) {
	return r.Run(ctx, name, args...)
}
