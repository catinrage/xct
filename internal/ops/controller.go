package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xct/internal/domain"
	"xct/internal/generate"
	"xct/internal/profile"
)

type Controller struct {
	Store  profile.Store
	Runner Runner
}

type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
	StepInfo    StepStatus = "info"
)

type ProgressEvent struct {
	Step   string
	Status StepStatus
	Detail string
	Err    string
}

type ProgressFunc func(ProgressEvent)

type deployStep struct {
	name string
	run  func() error
}

type DependencyStatus struct {
	Missing []string
}

func (d DependencyStatus) OK() bool {
	return len(d.Missing) == 0
}

func New(baseDir string) Controller {
	return Controller{
		Store:  profile.NewStore(baseDir),
		Runner: ExecRunner{},
	}
}

func (c Controller) CheckLocalDependencies(ctx context.Context) DependencyStatus {
	required := []string{"nginx", "ssh", "curl", "sshpass", "ncat", "nc"}
	var missing []string
	for _, cmd := range required {
		if _, err := c.Runner.Run(ctx, "sh", "-c", "command -v "+shellQuote(cmd)); err != nil {
			missing = append(missing, cmd)
		}
	}
	return DependencyStatus{Missing: missing}
}

func (c Controller) InstallLocalDependencies(ctx context.Context) (string, error) {
	var log strings.Builder
	if os.Geteuid() != 0 {
		return "", errors.New("install dependencies must be run as root on the Iran VPS")
	}
	if err := runRequired(ctx, &log, c.Runner, "sh", "-c", "apt-get update && apt-get install -y nginx curl openssh-client sshpass ncat netcat-openbsd"); err != nil {
		return log.String(), err
	}
	appendText(&log, "Local dependencies installed.")
	return log.String(), nil
}

func (c Controller) Create(ctx context.Context, p domain.Profile, applyTuning bool) (string, error) {
	return c.CreateWithProgress(ctx, p, applyTuning, false, nil)
}

func (c Controller) CreateWithProgress(ctx context.Context, p domain.Profile, applyTuning bool, resume bool, progress ProgressFunc) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("create must be run as root on the Iran VPS")
	}
	p.FillDerivedPaths(c.Store.BaseDir)
	if err := p.Validate(); err != nil {
		return "", err
	}
	if err := c.Store.Ensure(); err != nil {
		return "", err
	}
	if _, err := os.Stat(p.SSLCrt); err != nil {
		return "", fmt.Errorf("SSL certificate not found: %s", p.SSLCrt)
	}
	if _, err := os.Stat(p.SSLKey); err != nil {
		return "", fmt.Errorf("SSL key not found: %s", p.SSLKey)
	}

	var log strings.Builder
	files, err := generate.All(p)
	if err != nil {
		return "", err
	}
	p.DeployStatus = "in_progress"
	if !resume {
		p.FailedStep = ""
	}
	if err := c.Store.Save(p); err != nil {
		return "", err
	}
	appendText(&log, "Saved profile state for rescue: "+p.Profile)

	steps := []deployStep{
		{name: "Write Iran nginx site", run: func() error { return os.WriteFile(p.IranNginxSite, files.NginxSite, 0o644) }},
		{name: "Enable Iran nginx site", run: func() error { return ensureSymlink(p.IranNginxSite, generate.NginxEnabledPath(p.IranNginxSite)) }},
		{name: "Write Iran Xray config", run: func() error { return os.WriteFile(p.IranXrayConfig, files.IranConfig, 0o600) }},
		{name: "Write Iran systemd service", run: func() error {
			return os.WriteFile(filepath.Join("/etc/systemd/system", p.IranService), files.IranService, 0o644)
		}},
		{name: "Test Iran Xray config", run: func() error {
			return runRequired(ctx, &log, c.Runner, p.XrayBin, "run", "-test", "-config", p.IranXrayConfig)
		}},
		{name: "Test nginx config", run: func() error { return runRequired(ctx, &log, c.Runner, "nginx", "-t") }},
		{name: "Reload nginx", run: func() error { return runRequired(ctx, &log, c.Runner, "systemctl", "reload", "nginx") }},
		{name: "Reload systemd", run: func() error { return runRequired(ctx, &log, c.Runner, "systemctl", "daemon-reload") }},
		{name: "Start Iran Xray service", run: func() error { return runRequired(ctx, &log, c.Runner, "systemctl", "enable", "--now", p.IranService) }},
		{name: "Connect to outer VPS and create directories", run: func() error {
			return remoteRequired(ctx, &log, c, p, "mkdir -p "+shellQuote(c.Store.BaseDir)+" "+shellQuote(c.Store.ProfilesDir))
		}},
		{name: "Install/check nginx and Xray on outer VPS", run: func() error {
			return remoteRequired(ctx, &log, c, p, remoteBootstrapCommand(p.RemoteXrayBin))
		}},
		{name: "Validate outer local ports", run: func() error {
			return remoteRequired(ctx, &log, c, p, remotePortCheckCommand(p, resume))
		}},
		{name: "Upload outer Xray config", run: func() error {
			return remotePutRequired(ctx, &log, c, p, files.OuterConfig, p.OuterXrayConfig, "600")
		}},
		{name: "Upload outer systemd service", run: func() error {
			return remotePutRequired(ctx, &log, c, p, files.OuterService, filepath.Join("/etc/systemd/system", p.OuterService), "644")
		}},
		{name: "Test outer Xray config", run: func() error {
			return remoteRequired(ctx, &log, c, p, shellQuote(p.RemoteXrayBin)+" run -test -config "+shellQuote(p.OuterXrayConfig))
		}},
		{name: "Start outer Xray service", run: func() error {
			return remoteRequired(ctx, &log, c, p, "systemctl daemon-reload && systemctl enable --now "+shellQuote(p.OuterService))
		}},
	}
	if applyTuning {
		steps = append(steps,
			deployStep{name: "Load BBR module on Iran", run: func() error {
				appendRun(ctx, &log, c.Runner, "modprobe", "tcp_bbr")
				return nil
			}},
			deployStep{name: "Apply OS tuning on Iran", run: func() error { return runRequired(ctx, &log, c.Runner, "sh", "-c", tuningCommand()) }},
			deployStep{name: "Apply OS tuning on outer VPS", run: func() error { return remoteRequired(ctx, &log, c, p, tuningCommand()) }},
		)
	}

	start := 0
	if resume && p.FailedStep != "" {
		for i, step := range steps {
			if step.name == p.FailedStep {
				start = i
				break
			}
		}
		appendText(&log, "Resuming from step: "+p.FailedStep)
		if progress != nil {
			progress(ProgressEvent{Step: p.FailedStep, Status: StepInfo, Detail: "resuming from last failed step"})
		}
	}
	for i := start; i < len(steps); i++ {
		step := steps[i]
		if progress != nil {
			progress(ProgressEvent{Step: step.name, Status: StepRunning})
		}
		appendText(&log, "==> "+step.name)
		if err := step.run(); err != nil {
			p.DeployStatus = "failed"
			p.FailedStep = step.name
			_ = c.Store.Save(p)
			if progress != nil {
				progress(ProgressEvent{Step: step.name, Status: StepFailed, Err: err.Error()})
			}
			return log.String(), err
		}
		if progress != nil {
			progress(ProgressEvent{Step: step.name, Status: StepDone})
		}
	}

	p.DeployStatus = "complete"
	p.FailedStep = ""
	_ = c.Store.Save(p)
	appendText(&log, "Created profile "+p.Profile)
	return log.String(), nil
}

func (c Controller) Resume(ctx context.Context, name string, applyTuning bool) (string, error) {
	return c.ResumeWithProgress(ctx, name, applyTuning, nil)
}

func (c Controller) ResumeWithProgress(ctx context.Context, name string, applyTuning bool, progress ProgressFunc) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	if p.DeployStatus == "complete" && p.FailedStep == "" {
		return "Nothing to rescue. Profile deployment is marked complete.\n", nil
	}
	return c.CreateWithProgress(ctx, p, applyTuning, true, progress)
}

func (c Controller) List() ([]domain.Profile, error) {
	return c.Store.List()
}

func (c Controller) Load(name string) (domain.Profile, error) {
	return c.Store.Load(name)
}

func (c Controller) Show(name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	return describe(p), nil
}

func (c Controller) Outbound(name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	return generate.OutboundSnippet(p), nil
}

func (c Controller) Manage(ctx context.Context, action, name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	var log strings.Builder
	appendRun(ctx, &log, c.Runner, "systemctl", action, p.IranService)
	appendRemote(ctx, &log, c, p, "systemctl "+shellQuote(action)+" "+shellQuote(p.OuterService))
	return log.String(), nil
}

func (c Controller) Status(ctx context.Context, name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	var log strings.Builder
	appendText(&log, "--- Iran service ---")
	appendRun(ctx, &log, c.Runner, "systemctl", "status", p.IranService, "--no-pager")
	appendText(&log, "--- Iran listeners ---")
	appendRun(ctx, &log, c.Runner, "sh", "-c", listenerCommand(p))
	appendText(&log, "--- Outer service ---")
	appendRemote(ctx, &log, c, p, "systemctl status "+shellQuote(p.OuterService)+" --no-pager || true")
	appendText(&log, "--- Outer listeners ---")
	appendRemote(ctx, &log, c, p, "ss -lntp | grep -E '"+outerListenerExpr(p)+"' || true")
	return log.String(), nil
}

func (c Controller) Test(ctx context.Context, name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	var log strings.Builder
	appendText(&log, "[1] Iran service/listeners")
	appendRun(ctx, &log, c.Runner, "systemctl", "is-active", "--quiet", p.IranService)
	appendRun(ctx, &log, c.Runner, "sh", "-c", listenerCommand(p))
	appendRun(ctx, &log, c.Runner, p.XrayBin, "run", "-test", "-config", p.IranXrayConfig)
	appendRun(ctx, &log, c.Runner, "nginx", "-t")
	appendText(&log, "[2] Iran backend WebSocket")
	appendRun(ctx, &log, c.Runner, "curl", wsArgs("http://127.0.0.1:"+strconv.Itoa(p.BackendPort)+p.WSPath)...)
	appendText(&log, "[3] Iran public/CDN path")
	appendRun(ctx, &log, c.Runner, "curl", wsArgs("https://"+p.Domain+":"+strconv.Itoa(p.CDNPort)+p.WSPath)...)
	appendText(&log, "[4] Outer service/listeners")
	appendRemote(ctx, &log, c, p, "systemctl is-active --quiet "+shellQuote(p.OuterService)+" && echo OK: outer service active || echo FAIL: outer service inactive")
	appendRemote(ctx, &log, c, p, "ss -lntp | grep -E '"+outerListenerExpr(p)+"' || true")
	appendText(&log, "[5] Outer public WebSocket to Iran/CDN")
	appendRemote(ctx, &log, c, p, "curl "+strings.Join(shellQuoteArgs(wsArgs("https://"+p.Domain+":"+strconv.Itoa(p.CDNPort)+p.WSPath)), " ")+" || true")
	if p.Type == domain.Reverse {
		appendText(&log, "[6] End-to-end reverse: Iran local SOCKS exits outer")
		appendRun(ctx, &log, c.Runner, "curl", "-4", "-sS", "--proxy", fmt.Sprintf("socks5h://%s:%s@127.0.0.1:%d", p.IranSocksUser, p.IranSocksPass, p.IranSocksPort), domain.ChabokanURL, "--max-time", "25")
	} else {
		appendText(&log, "[6] End-to-end direct: outer local SOCKS exits Iran")
		appendRemote(ctx, &log, c, p, fmt.Sprintf("curl -4 -sS --proxy %s %s --max-time 25 || true", shellQuote(fmt.Sprintf("socks5h://%s:%s@127.0.0.1:%d", p.OuterSocksUser, p.OuterSocksPass, p.OuterSocksPort)), shellQuote(domain.ChabokanURL)))
	}
	return log.String(), nil
}

func (c Controller) Debug(ctx context.Context, name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	var log strings.Builder
	appendText(&log, "--- profile ---")
	appendText(&log, describe(p))
	appendText(&log, "--- Iran status ---")
	appendRun(ctx, &log, c.Runner, "systemctl", "status", p.IranService, "--no-pager")
	appendText(&log, "--- Iran recent logs ---")
	appendRun(ctx, &log, c.Runner, "journalctl", "-u", p.IranService, "-n", "160", "--no-pager")
	appendText(&log, "--- Iran nginx error log ---")
	appendRun(ctx, &log, c.Runner, "tail", "-n", "120", "/var/log/nginx/error.log")
	appendText(&log, "--- Outer status ---")
	appendRemote(ctx, &log, c, p, "systemctl status "+shellQuote(p.OuterService)+" --no-pager || true")
	appendText(&log, "--- Outer recent logs ---")
	appendRemote(ctx, &log, c, p, "journalctl -u "+shellQuote(p.OuterService)+" -n 160 --no-pager || true")
	test, _ := c.Test(ctx, name)
	appendText(&log, "--- layered test ---")
	appendText(&log, test)
	return log.String(), nil
}

func (c Controller) GenerateLog(ctx context.Context, name string) (string, error) {
	output, err := c.Debug(ctx, name)
	if err != nil {
		return output, err
	}
	path := filepath.Join(c.Store.ProfileDir(name), name+".log")
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return output, mkErr
	}
	if writeErr := os.WriteFile(path, []byte(output), 0o600); writeErr != nil {
		return output, writeErr
	}
	return "Generated diagnostic log:\n" + path + "\n\n" + output, nil
}

func (c Controller) Tune(ctx context.Context, name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	var log strings.Builder
	appendRun(ctx, &log, c.Runner, "sh", "-c", tuningCommand())
	appendRemote(ctx, &log, c, p, tuningCommand())
	return log.String(), nil
}

func (c Controller) Delete(ctx context.Context, name string) (string, error) {
	p, err := c.Load(name)
	if err != nil {
		return "", err
	}
	var log strings.Builder
	appendRun(ctx, &log, c.Runner, "systemctl", "disable", "--now", p.IranService)
	_ = os.Remove(filepath.Join("/etc/systemd/system", p.IranService))
	_ = os.Remove(p.IranXrayConfig)
	_ = os.Remove(p.IranNginxSite)
	_ = os.Remove(generate.NginxEnabledPath(p.IranNginxSite))
	appendRun(ctx, &log, c.Runner, "nginx", "-t")
	appendRun(ctx, &log, c.Runner, "systemctl", "reload", "nginx")
	appendRun(ctx, &log, c.Runner, "systemctl", "daemon-reload")
	appendRemote(ctx, &log, c, p, "systemctl disable --now "+shellQuote(p.OuterService)+" 2>/dev/null || true; rm -f /etc/systemd/system/"+shellQuote(p.OuterService)+" "+shellQuote(p.OuterXrayConfig)+"; systemctl daemon-reload")
	_ = os.RemoveAll(c.Store.ProfileDir(p.Profile))
	appendText(&log, "Deleted profile "+p.Profile)
	return log.String(), nil
}

func (c Controller) RemoteCommand(ctx context.Context, p domain.Profile, cmd string) (string, error) {
	args := sshArgs(p)
	quoted := strconv.Quote(cmd)
	remote := "bash -lc " + quoted
	if p.RemoteRootMode != "root" {
		remote = "sudo -n " + remote
	}
	args = append(args, p.SSHUser+"@"+p.SSHHost, remote)
	if p.SSHAuth == domain.SSHPassword {
		password := os.Getenv("XCT_SSH_PASSWORD")
		if password == "" {
			return "", errors.New("outer SSH password is required for this password-auth profile")
		}
		args = append([]string{"-p", password, "ssh"}, args...)
		return c.Runner.Run(ctx, "sshpass", args...)
	}
	return c.Runner.Run(ctx, "ssh", args...)
}

func (c Controller) RemotePut(ctx context.Context, p domain.Profile, content []byte, dest, mode string) (string, error) {
	tmp := "/tmp/xct-upload-" + filepath.Base(dest)
	args := sshArgs(p)
	args = append(args, p.SSHUser+"@"+p.SSHHost, "cat > "+shellQuote(tmp))
	var out string
	var err error
	if p.SSHAuth == domain.SSHPassword {
		password := os.Getenv("XCT_SSH_PASSWORD")
		if password == "" {
			return "", errors.New("outer SSH password is required for this password-auth profile")
		}
		args = append([]string{"-p", password, "ssh"}, args...)
		out, err = c.Runner.RunInput(ctx, content, "sshpass", args...)
	} else {
		out, err = c.Runner.RunInput(ctx, content, "ssh", args...)
	}
	if err != nil {
		return out, err
	}
	next, err := c.RemoteCommand(ctx, p, "install -m "+mode+" "+shellQuote(tmp)+" "+shellQuote(dest)+"; rm -f "+shellQuote(tmp))
	return out + next, err
}

func appendRemote(ctx context.Context, b *strings.Builder, c Controller, p domain.Profile, cmd string) {
	out, err := c.RemoteCommand(ctx, p, cmd)
	appendCommandResult(b, "remote: "+cmd, out, err)
}

func appendRemotePut(ctx context.Context, b *strings.Builder, c Controller, p domain.Profile, content []byte, dest, mode string) {
	out, err := c.RemotePut(ctx, p, content, dest, mode)
	appendCommandResult(b, "remote put: "+dest, out, err)
}

func appendRun(ctx context.Context, b *strings.Builder, runner Runner, name string, args ...string) {
	out, err := runner.Run(ctx, name, args...)
	appendCommandResult(b, strings.Join(append([]string{name}, args...), " "), out, err)
}

func runRequired(ctx context.Context, b *strings.Builder, runner Runner, name string, args ...string) error {
	out, err := runner.Run(ctx, name, args...)
	appendCommandResult(b, strings.Join(append([]string{name}, args...), " "), out, err)
	if err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func remoteRequired(ctx context.Context, b *strings.Builder, c Controller, p domain.Profile, cmd string) error {
	out, err := c.RemoteCommand(ctx, p, cmd)
	appendCommandResult(b, "remote: "+cmd, out, err)
	if err != nil {
		return fmt.Errorf("remote command failed: %w", err)
	}
	return nil
}

func remotePutRequired(ctx context.Context, b *strings.Builder, c Controller, p domain.Profile, content []byte, dest, mode string) error {
	out, err := c.RemotePut(ctx, p, content, dest, mode)
	appendCommandResult(b, "remote put: "+dest, out, err)
	if err != nil {
		return fmt.Errorf("remote upload failed: %w", err)
	}
	return nil
}

func appendCommandResult(b *strings.Builder, label, out string, err error) {
	appendText(b, "$ "+label)
	if strings.TrimSpace(out) != "" {
		appendText(b, out)
	}
	if err != nil {
		appendText(b, "WARN: "+err.Error())
	}
}

func appendText(b *strings.Builder, text string) {
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
}

func writeStep(b *strings.Builder, first *error, err error, label string) {
	if err != nil && *first == nil {
		*first = fmt.Errorf("%s: %w", label, err)
	}
	appendCommandResult(b, label, "", err)
}

func ensureSymlink(target, link string) error {
	_ = os.Remove(link)
	return os.Symlink(target, link)
}

func sshArgs(p domain.Profile) []string {
	args := []string{"-p", strconv.Itoa(p.SSHPort), "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3", "-o", "StrictHostKeyChecking=accept-new"}
	if p.SSHSOCKSEnabled {
		args = append(args, "-o", "ProxyCommand="+proxyCommand(p))
	}
	if p.SSHAuth == domain.SSHKey {
		args = append(args, "-o", "BatchMode=yes")
		if p.SSHKey != "" {
			args = append(args, "-i", p.SSHKey)
		}
	}
	return args
}

func proxyCommand(p domain.Profile) string {
	if p.SSHSOCKSUser == "" {
		return fmt.Sprintf("nc -X 5 -x %s:%d %%h %%p", p.SSHSOCKSHost, p.SSHSOCKSPort)
	}
	if p.SSHSOCKSPass == "" {
		return fmt.Sprintf("nc -X 5 -x %s:%d -P %s %%h %%p", p.SSHSOCKSHost, p.SSHSOCKSPort, p.SSHSOCKSUser)
	}
	return fmt.Sprintf("ncat --proxy-type socks5 --proxy %s:%d --proxy-auth %s:%s %%h %%p", p.SSHSOCKSHost, p.SSHSOCKSPort, p.SSHSOCKSUser, p.SSHSOCKSPass)
}

func remoteBootstrapCommand(remoteXrayBin string) string {
	quotedBin := shellQuote(remoteXrayBin)
	quotedDir := shellQuote(filepath.Dir(remoteXrayBin))
	return fmt.Sprintf(`set -e
export DEBIAN_FRONTEND=noninteractive
install_pkg() {
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    apt-get install -y "$@"
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y "$@"
  elif command -v yum >/dev/null 2>&1; then
    yum install -y "$@"
  else
    echo "ERROR: no supported package manager found for outer bootstrap" >&2
    return 1
  fi
}
if ! command -v curl >/dev/null 2>&1; then
  install_pkg curl ca-certificates
fi
if command -v apt-get >/dev/null 2>&1; then
  apt-get update
  apt-get install unzip -y
elif ! command -v unzip >/dev/null 2>&1; then
  install_pkg unzip
fi
if ! command -v nginx >/dev/null 2>&1; then
  install_pkg nginx
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl enable --now nginx || true
fi
if ! test -x %[1]s; then
  bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install --without-geodata
fi
if ! test -x %[1]s && test -x /usr/local/bin/xray; then
  mkdir -p %[2]s
  ln -sf /usr/local/bin/xray %[1]s
fi
test -x %[1]s`, quotedBin, quotedDir)
}

func remotePortCheckCommand(p domain.Profile, resume bool) string {
	ports := []int{}
	if p.Type == domain.Direct {
		ports = uniquePositivePorts([]int{p.OuterLocalVLESSPort, p.OuterSocksPort})
	}
	if len(ports) == 0 {
		return "true"
	}
	var b strings.Builder
	b.WriteString("set -e\n")
	if resume {
		b.WriteString("if systemctl is-active --quiet " + shellQuote(p.OuterService) + "; then echo 'outer service already active; skipping port-free check for rescue'; exit 0; fi\n")
	}
	for _, port := range ports {
		fmt.Fprintf(&b, "if ss -lnt | awk '{print $4}' | grep -Eq '[:.]%d$'; then echo 'outer TCP port %d is already listening' >&2; exit 1; fi\n", port, port)
	}
	return b.String()
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellQuoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = shellQuote(arg)
	}
	return out
}

func wsArgs(url string) []string {
	return []string{
		"--http1.1", "-skv",
		"-H", "Connection: Upgrade",
		"-H", "Upgrade: websocket",
		"-H", "Sec-WebSocket-Version: 13",
		"-H", "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==",
		url, "--max-time", "8",
	}
}

func listenerCommand(p domain.Profile) string {
	expr := fmt.Sprintf(":(%d|%d", p.CDNPort, p.BackendPort)
	if p.IranSocksPort > 0 {
		expr += fmt.Sprintf("|%d", p.IranSocksPort)
	}
	if p.IranLocalVLESSPort > 0 {
		expr += fmt.Sprintf("|%d", p.IranLocalVLESSPort)
	}
	expr += ")"
	return "ss -lntp | grep -E '" + expr + "' || true"
}

func outerListenerExpr(p domain.Profile) string {
	if p.Type == domain.Direct {
		return fmt.Sprintf(":(%d|%d)", p.OuterLocalVLESSPort, p.OuterSocksPort)
	}
	return ":(0)"
}

func tuningCommand() string {
	return `modprobe tcp_bbr 2>/dev/null || true
cc=bbr
if ! sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null | tr " " "\n" | grep -qx bbr; then
  cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || printf cubic)
  echo "WARN: BBR unavailable; keeping congestion control: $cc"
fi
cat > /etc/sysctl.d/99-tunnel-speed.conf <<EOF
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=$cc

net.core.rmem_max=134217728
net.core.wmem_max=134217728

net.ipv4.tcp_rmem=4096 87380 67108864
net.ipv4.tcp_wmem=4096 65536 67108864

net.ipv4.tcp_mtu_probing=1
net.ipv4.tcp_slow_start_after_idle=0

net.core.somaxconn=65535
net.ipv4.ip_local_port_range=10000 65000

net.ipv4.tcp_keepalive_time=300
net.ipv4.tcp_keepalive_intvl=30
net.ipv4.tcp_keepalive_probes=5
net.ipv4.tcp_max_syn_backlog=65535
net.core.netdev_max_backlog=250000
EOF
sysctl --system
sysctl net.ipv4.tcp_congestion_control net.core.default_qdisc 2>/dev/null || true`
}

func describe(p domain.Profile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Profile: %s\nType: %s\nDomain: %s\nCDN port: %d\nWS path: %s\n", p.Profile, p.Type, p.Domain, p.CDNPort, p.WSPath)
	fmt.Fprintf(&b, "Iran backend: 127.0.0.1:%d\nIran service: %s\nIran config: %s\nIran nginx site: %s\n", p.BackendPort, p.IranService, p.IranXrayConfig, p.IranNginxSite)
	fmt.Fprintf(&b, "Outer service: %s\nOuter config: %s\nOuter SSH: %s@%s:%d via SOCKS=%v\nRemote UUID: %s\n", p.OuterService, p.OuterXrayConfig, p.SSHUser, p.SSHHost, p.SSHPort, p.SSHSOCKSEnabled, p.RemoteUUID)
	if p.Type == domain.Reverse {
		fmt.Fprintf(&b, "Iran SOCKS: %s:%d user=%s pass=%s\n", p.IranSocksListen, p.IranSocksPort, p.IranSocksUser, p.IranSocksPass)
		fmt.Fprintf(&b, "Iran local VLESS: %s:%d uuid=%s\n", p.IranLocalVLESSListen, p.IranLocalVLESSPort, p.IranLocalVLESSUUID)
	} else {
		fmt.Fprintf(&b, "Outer local VLESS: %s:%d uuid=%s\n", p.OuterLocalVLESSListen, p.OuterLocalVLESSPort, p.OuterLocalVLESSUUID)
		fmt.Fprintf(&b, "Outer local SOCKS: %s:%d user=%s pass=%s\n", p.OuterSocksListen, p.OuterSocksPort, p.OuterSocksUser, p.OuterSocksPass)
	}
	return b.String()
}
