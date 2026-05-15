package tui

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"xct/internal/build"
	"xct/internal/domain"
	"xct/internal/ops"
	"xct/internal/settings"
	"xct/internal/update"
)

type action int

const (
	actionCreateReverse action = iota
	actionCreateDirect
	actionList
	actionShow
	actionOutbound
	actionTest
	actionDebug
	actionStatus
	actionStart
	actionStop
	actionRestart
	actionEnable
	actionDisable
	actionTune
	actionDelete
	actionRescue
	actionGenerateLog
	actionUpdate
	actionSettings
	actionInstallDependencies
	actionEditSettings
	actionTestUpdateSocks
	actionProfilePassword
	actionBack
	actionQuit
)

type menuItem struct {
	title       string
	help        string
	act         action
	profileName string
	separator   bool
	disabled    bool
}

type mode int

const (
	modeMenu mode = iota
	modeForm
	modeProfilePrompt
	modeRunning
	modeOutput
)

type field struct {
	key         string
	label       string
	value       string
	placeholder string
	secret      bool
	checkbox    bool
	choices     []string
}

type resultMsg struct {
	output string
	err    error
}

type progressMsg struct {
	event ops.ProgressEvent
}

type progressDoneMsg struct {
	output string
	err    error
}

type Model struct {
	controller     ops.Controller
	buildInfo      build.Info
	settingsStore  settings.Store
	mode           mode
	menu           []menuItem
	selected       int
	profiles       []domain.Profile
	inProfile      bool
	activeProfile  string
	pendingAction  action
	pendingProfile string
	runningAction  action

	formAction action
	allFields  []field
	fields     []field
	inputs     []textinput.Model
	focus      int
	helpOpen   bool

	spinner    spinner.Model
	viewport   viewport.Model
	output     string
	err        error
	width      int
	height     int
	progress   []ops.ProgressEvent
	progressCh <-chan tea.Msg
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("45"))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1, 2)
)

func Run(baseDir string, info build.Info) error {
	ctrl := ops.New(baseDir)
	m := NewModel(ctrl, info)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func NewModel(ctrl ops.Controller, info build.Info) Model {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	vp := viewport.New(100, 24)
	return Model{
		controller:    ctrl,
		buildInfo:     info,
		settingsStore: settings.NewStore(ctrl.Store.BaseDir),
		mode:          modeMenu,
		spinner:       spin,
		viewport:      vp,
	}.withMainMenu()
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = max(40, msg.Width-4)
		m.viewport.Height = max(10, msg.Height-8)
	case tea.KeyMsg:
		switch m.mode {
		case modeMenu:
			return m.updateMenu(msg)
		case modeForm, modeProfilePrompt:
			return m.updateForm(msg)
		case modeOutput:
			switch msg.String() {
			case "esc", "q":
				m.mode = modeMenu
				if m.inProfile && m.activeProfile != "" {
					m.menu = profileActionMenu(m.activeProfile)
					m.selected = m.firstSelectable()
				} else {
					m = m.withMainMenu()
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case modeRunning:
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case resultMsg:
		m.mode = modeOutput
		m.output = msg.output
		m.err = msg.err
		if msg.err != nil {
			m.output += "\nERROR: " + msg.err.Error() + "\n"
		}
		if m.runningAction == actionDelete && msg.err == nil {
			m.inProfile = false
			m.activeProfile = ""
		}
		m.runningAction = actionQuit
		m.viewport.SetContent(m.output)
		m.viewport.GotoTop()
		return m, nil
	case progressMsg:
		m.upsertProgress(msg.event)
		return m, waitProgressCmd(m.progressCh)
	case progressDoneMsg:
		m.mode = modeOutput
		m.output = msg.output
		m.err = msg.err
		if msg.err != nil {
			m.output += "\nERROR: " + msg.err.Error() + "\n"
		}
		m.viewport.SetContent(m.output)
		m.viewport.GotoTop()
		return m, nil
	}
	return m, nil
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		m.moveSelection(-1)
	case "down", "j":
		m.moveSelection(1)
	case "enter":
		item := m.menu[m.selected]
		if item.separator {
			return m, nil
		}
		if item.disabled {
			return m, nil
		}
		if item.act == actionQuit {
			return m, tea.Quit
		}
		if item.profileName != "" {
			m.activeProfile = item.profileName
			m.inProfile = true
			m.menu = profileActionMenu(item.profileName)
			m.selected = 0
			return m, nil
		}
		return m.startAction(item.act)
	}
	return m, nil
}

func (m Model) withMainMenu() Model {
	profiles, err := m.controller.List()
	if err == nil {
		m.profiles = profiles
	}
	m.inProfile = false
	m.activeProfile = ""
	deps := m.controller.CheckLocalDependencies(context.Background())
	createHelp := "Iran local SOCKS/VLESS exits through outer VPS"
	createDirectHelp := "Outer local SOCKS/VLESS exits through Iran VPS"
	createDisabled := false
	if !deps.OK() {
		createDisabled = true
		missing := strings.Join(deps.Missing, ", ")
		createHelp = "disabled: install missing dependencies first: " + missing
		createDirectHelp = createHelp
	}
	m.menu = []menuItem{
		{title: "Create reverse profile", help: createHelp, act: actionCreateReverse, disabled: createDisabled},
		{title: "Create direct profile", help: createDirectHelp, act: actionCreateDirect, disabled: createDisabled},
		{title: "Install dependencies", help: "Install nginx, sshpass, ncat, netcat-openbsd, curl", act: actionInstallDependencies},
		{title: "Update", help: "Check GitHub releases and install latest build", act: actionUpdate},
		{title: "Settings", help: "Configure update SOCKS5 proxy", act: actionSettings},
		{separator: true},
	}
	if len(m.profiles) == 0 {
		m.menu = append(m.menu, menuItem{title: "No profiles yet", help: "Create a reverse or direct profile above", separator: true})
	} else {
		for _, p := range m.profiles {
			m.menu = append(m.menu, menuItem{
				title:       p.Profile,
				help:        fmt.Sprintf("%s  %s:%d%s", p.Type, p.Domain, p.CDNPort, p.WSPath),
				profileName: p.Profile,
			})
		}
	}
	m.menu = append(m.menu, menuItem{separator: true}, menuItem{title: "Quit", help: "Exit", act: actionQuit})
	if m.selected >= len(m.menu) {
		m.selected = len(m.menu) - 1
	}
	if m.selected < 0 || m.menu[m.selected].separator {
		m.selected = m.firstSelectable()
	}
	return m
}

func profileActionMenu(name string) []menuItem {
	return []menuItem{
		{title: "Show profile", help: "Display endpoints, files, and services", act: actionShow},
		{title: "Outbound snippet", help: "Print x-ui outbound JSON", act: actionOutbound},
		{title: "Test", help: "Run layered local and remote tunnel checks", act: actionTest},
		{title: "Debug", help: "Print status, logs, listeners, and tests", act: actionDebug},
		{title: "Generate log", help: "Create a profile diagnostic log file", act: actionGenerateLog},
		{title: "Status", help: "Show service and listener state", act: actionStatus},
		{separator: true},
		{title: "Start", help: "Start Iran and outer services", act: actionStart},
		{title: "Stop", help: "Stop Iran and outer services", act: actionStop},
		{title: "Restart", help: "Restart Iran and outer services", act: actionRestart},
		{title: "Enable", help: "Enable Iran and outer services", act: actionEnable},
		{title: "Disable", help: "Disable Iran and outer services", act: actionDisable},
		{separator: true},
		{title: "Rescue", help: "Resume creation from saved profile state", act: actionRescue},
		{title: "Tune", help: "Apply TCP tuning on Iran and outer", act: actionTune},
		{title: "Delete", help: "Remove generated files and services", act: actionDelete},
		{separator: true},
		{title: "Back", help: "Return to profile list", act: actionBack},
		{title: "Quit", help: "Exit", act: actionQuit},
	}
}

func settingsActionMenu() []menuItem {
	return []menuItem{
		{title: "Edit update SOCKS", help: "Set SOCKS5 proxy used for GitHub checks/downloads", act: actionEditSettings},
		{title: "Test update SOCKS", help: "Check GitHub release access with current settings", act: actionTestUpdateSocks},
		{separator: true},
		{title: "Back", help: "Return to profile list", act: actionBack},
		{title: "Quit", help: "Exit", act: actionQuit},
	}
}

func (m *Model) moveSelection(delta int) {
	if len(m.menu) == 0 {
		return
	}
	next := m.selected
	for {
		next += delta
		if next < 0 || next >= len(m.menu) {
			return
		}
		if !m.menu[next].separator && !m.menu[next].disabled {
			m.selected = next
			return
		}
	}
}

func (m Model) firstSelectable() int {
	for i, item := range m.menu {
		if !item.separator && !item.disabled {
			return i
		}
	}
	return 0
}

func (m *Model) focusField(next int) {
	if next < 0 || next >= len(m.fields) {
		return
	}
	if m.focus >= 0 && m.focus < len(m.inputs) {
		m.inputs[m.focus].Blur()
	}
	m.focus = next
	if !m.fields[m.focus].checkbox && len(m.fields[m.focus].choices) == 0 {
		m.inputs[m.focus].Focus()
	}
}

func (m Model) collectFormValues() map[string]string {
	values := map[string]string{}
	for _, f := range m.allFields {
		values[f.key] = f.value
	}
	for i, f := range m.fields {
		if f.checkbox || len(f.choices) > 0 {
			values[f.key] = f.value
			continue
		}
		if i < len(m.inputs) {
			values[f.key] = m.inputs[i].Value()
		}
	}
	return values
}

func (m *Model) syncAllFields(values map[string]string) {
	for i := range m.allFields {
		if value, ok := values[m.allFields[i].key]; ok {
			m.allFields[i].value = value
		}
	}
}

func (m *Model) rebuildVisibleFields(preferredKey string) {
	values := m.collectFormValues()
	m.syncAllFields(values)
	m.fields = m.visibleFields(values)
	m.inputs = make([]textinput.Model, len(m.fields))
	m.focus = 0
	for i, f := range m.fields {
		input := textinput.New()
		input.Placeholder = f.placeholder
		input.SetValue(f.value)
		input.Prompt = "  "
		input.CharLimit = 512
		if f.secret {
			input.EchoMode = textinput.EchoPassword
			input.EchoCharacter = '*'
		}
		m.inputs[i] = input
		if f.key == preferredKey {
			m.focus = i
		}
	}
	if len(m.fields) > 0 && m.focus >= len(m.fields) {
		m.focus = len(m.fields) - 1
	}
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
	if len(m.fields) > 0 && !m.fields[m.focus].checkbox && len(m.fields[m.focus].choices) == 0 {
		m.inputs[m.focus].Focus()
	}
}

func (m Model) visibleFields(values map[string]string) []field {
	var visible []field
	for _, f := range m.allFields {
		if m.fieldHidden(f, values) {
			continue
		}
		if value, ok := values[f.key]; ok {
			f.value = value
		}
		visible = append(visible, f)
	}
	return visible
}

func (m Model) fieldHidden(f field, values map[string]string) bool {
	switch f.key {
	case "use_update_socks":
		return !m.settingsSOCKSAvailable()
	case "ssh_password":
		if m.formAction == actionProfilePassword {
			return false
		}
		return values["ssh_auth"] != string(domain.SSHPassword)
	case "ssh_key":
		return values["ssh_auth"] == string(domain.SSHPassword)
	case "ssh_socks_enabled", "ssh_socks_host", "ssh_socks_port", "ssh_socks_user", "ssh_socks_pass":
		if yes(values["use_update_socks"]) {
			return true
		}
		if f.key == "ssh_socks_enabled" {
			return false
		}
		if !yes(values["ssh_socks_enabled"]) {
			return true
		}
		if f.key == "ssh_socks_pass" && strings.TrimSpace(values["ssh_socks_user"]) == "" {
			return true
		}
	case "update_socks_host", "update_socks_port", "update_socks_user", "update_socks_pass":
		if !yes(values["update_socks_enabled"]) {
			return true
		}
		if f.key == "update_socks_pass" && strings.TrimSpace(values["update_socks_user"]) == "" {
			return true
		}
	}
	return false
}

func (m Model) settingsSOCKSAvailable() bool {
	cfg, err := m.settingsStore.Load()
	if err != nil {
		return false
	}
	return cfg.UpdateSOCKSEnabled && cfg.UpdateSOCKSHost != "" && cfg.UpdateSOCKSPort > 0
}

func (m Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.helpOpen {
		switch msg.String() {
		case "ctrl+i", "tab", "esc", "enter", "q", " ":
			m.helpOpen = false
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeMenu
		return m, nil
	case "ctrl+i", "tab":
		m.helpOpen = true
		return m, nil
	case "enter":
		if m.focus == len(m.inputs)-1 {
			return m.submit()
		}
		m.focusField(m.focus + 1)
	case " ":
		if m.fields[m.focus].checkbox {
			m.fields[m.focus].value = toggleValue(m.fields[m.focus].value)
			m.rebuildVisibleFields(m.fields[m.focus].key)
			return m, nil
		}
		if len(m.fields[m.focus].choices) > 0 {
			m.fields[m.focus].value = nextChoice(m.fields[m.focus])
			m.rebuildVisibleFields(m.fields[m.focus].key)
			return m, nil
		}
	case "left", "h":
		if len(m.fields[m.focus].choices) > 0 {
			m.fields[m.focus].value = prevChoice(m.fields[m.focus])
			m.rebuildVisibleFields(m.fields[m.focus].key)
			return m, nil
		}
	case "right", "l":
		if len(m.fields[m.focus].choices) > 0 {
			m.fields[m.focus].value = nextChoice(m.fields[m.focus])
			m.rebuildVisibleFields(m.fields[m.focus].key)
			return m, nil
		}
	case "up":
		m.focusField(m.focus - 1)
	case "down":
		m.focusField(m.focus + 1)
	}

	if m.fields[m.focus].checkbox || len(m.fields[m.focus].choices) > 0 {
		return m, nil
	}
	var cmd tea.Cmd
	key := m.fields[m.focus].key
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	if m.reactiveTextField(key) {
		m.rebuildVisibleFields(key)
	}
	return m, cmd
}

func (m Model) reactiveTextField(key string) bool {
	switch key {
	case "ssh_socks_user", "update_socks_user":
		return true
	default:
		return false
	}
}

func (m Model) startAction(act action) (tea.Model, tea.Cmd) {
	m.runningAction = actionQuit
	switch act {
	case actionCreateReverse:
		return m.startForm(act, reverseFields())
	case actionCreateDirect:
		return m.startForm(act, directFields())
	case actionBack:
		return m.withMainMenu(), nil
	case actionSettings:
		m.inProfile = false
		m.activeProfile = ""
		m.menu = settingsActionMenu()
		m.selected = m.firstSelectable()
		return m, nil
	case actionEditSettings:
		return m.startForm(act, m.settingsFields())
	case actionTestUpdateSocks:
		m.mode = modeRunning
		return m, tea.Batch(m.spinner.Tick, runCmd(func() (string, error) {
			cfg, err := m.settingsStore.Load()
			if err != nil {
				return "", err
			}
			return update.Client{Settings: cfg, Build: m.buildInfo}.Check(context.Background())
		}))
	case actionInstallDependencies:
		m.mode = modeRunning
		return m, tea.Batch(m.spinner.Tick, runCmd(func() (string, error) {
			return m.controller.InstallLocalDependencies(context.Background())
		}))
	case actionUpdate:
		m.mode = modeRunning
		return m, tea.Batch(m.spinner.Tick, runCmd(func() (string, error) {
			cfg, err := m.settingsStore.Load()
			if err != nil {
				return "", err
			}
			return update.Client{Settings: cfg, Build: m.buildInfo}.InstallAndRestart(context.Background(), false)
		}))
	case actionRescue:
		profileName := m.activeProfile
		if m.shouldPromptProfilePassword(actionRescue, profileName) {
			return m.startProfilePasswordPrompt(actionRescue, profileName)
		}
		return m.startRescue(profileName)
	default:
		profileName := m.activeProfile
		if m.shouldPromptProfilePassword(act, profileName) {
			return m.startProfilePasswordPrompt(act, profileName)
		}
		return m.startProfileAction(act, profileName)
	}
}

func (m Model) startProfilePasswordPrompt(act action, profileName string) (tea.Model, tea.Cmd) {
	m.pendingAction = act
	m.pendingProfile = profileName
	return m.startForm(actionProfilePassword, []field{
		{key: "ssh_password", label: "Outer SSH password", secret: true},
	})
}

func (m Model) startProfileAction(act action, profileName string) (tea.Model, tea.Cmd) {
	m.mode = modeRunning
	m.runningAction = act
	return m, tea.Batch(m.spinner.Tick, runCmd(func() (string, error) {
		return m.runProfileAction(context.Background(), act, profileName, false)
	}))
}

func (m Model) startRescue(profileName string) (tea.Model, tea.Cmd) {
	return m.startProgress("Rescue "+profileName, func(progress ops.ProgressFunc) (string, error) {
		return m.controller.ResumeWithProgress(context.Background(), profileName, true, progress)
	})
}

func (m Model) shouldPromptProfilePassword(act action, profileName string) bool {
	if !profileActionNeedsRemote(act) || os.Getenv("XCT_SSH_PASSWORD") != "" || profileName == "" {
		return false
	}
	p, err := m.controller.Load(profileName)
	return err == nil && p.SSHAuth == domain.SSHPassword
}

func profileActionNeedsRemote(act action) bool {
	switch act {
	case actionShow, actionOutbound:
		return false
	default:
		return true
	}
}

func (m Model) startForm(act action, fields []field) (tea.Model, tea.Cmd) {
	m.mode = modeForm
	if act != actionCreateReverse && act != actionCreateDirect {
		m.mode = modeProfilePrompt
	}
	m.formAction = act
	m.allFields = fields
	m.fields = nil
	m.inputs = nil
	m.focus = 0
	m.helpOpen = false
	m.rebuildVisibleFields("")
	return m, textinput.Blink
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	values := m.collectFormValues()

	switch m.formAction {
	case actionProfilePassword:
		password := values["ssh_password"]
		if password == "" {
			m.mode = modeOutput
			m.err = fmt.Errorf("outer SSH password is required")
			m.output = "Outer SSH password is required for this password-auth profile.\n"
			m.viewport.SetContent(m.output)
			return m, nil
		}
		_ = os.Setenv("XCT_SSH_PASSWORD", password)
		if m.pendingAction == actionRescue {
			return m.startRescue(m.pendingProfile)
		}
		return m.startProfileAction(m.pendingAction, m.pendingProfile)
	case actionEditSettings:
		cfg := settingsFromValues(values)
		m.mode = modeRunning
		return m, tea.Batch(m.spinner.Tick, runCmd(func() (string, error) {
			if err := m.settingsStore.Save(cfg); err != nil {
				return "", err
			}
			return "Settings saved.\n", nil
		}))
	case actionCreateReverse, actionCreateDirect:
		if yes(values["use_update_socks"]) {
			cfg, err := m.settingsStore.Load()
			if err != nil {
				m.mode = modeOutput
				m.err = err
				m.output = "Could not load settings.\nERROR: " + err.Error() + "\n"
				m.viewport.SetContent(m.output)
				return m, nil
			}
			if !cfg.UpdateSOCKSEnabled || cfg.UpdateSOCKSHost == "" {
				m.mode = modeOutput
				m.err = fmt.Errorf("settings SOCKS5 is not enabled or has no host")
				m.output = "Cannot use settings SOCKS5 for this profile.\nERROR: settings SOCKS5 is not enabled or has no host\n"
				m.viewport.SetContent(m.output)
				return m, nil
			}
			values["ssh_socks_enabled"] = "yes"
			values["ssh_socks_host"] = cfg.UpdateSOCKSHost
			values["ssh_socks_port"] = strconv.Itoa(cfg.UpdateSOCKSPort)
			values["ssh_socks_user"] = cfg.UpdateSOCKSUser
			values["ssh_socks_pass"] = cfg.UpdateSOCKSPass
		}
		p, applyTuning, err := buildProfile(values, m.formAction)
		if err != nil {
			m.mode = modeOutput
			m.err = err
			m.output = "Invalid profile input.\nERROR: " + err.Error() + "\n"
			m.viewport.SetContent(m.output)
			return m, nil
		}
		if values["ssh_password"] != "" {
			_ = os.Setenv("XCT_SSH_PASSWORD", values["ssh_password"])
		}
		if err := m.controller.ValidateCreate(context.Background(), p); err != nil {
			m.mode = modeOutput
			m.err = err
			m.output = "Invalid profile input.\nERROR: " + err.Error() + "\n"
			m.viewport.SetContent(m.output)
			return m, nil
		}
		return m.startProgress("Create "+p.Profile, func(progress ops.ProgressFunc) (string, error) {
			return m.controller.CreateWithProgress(context.Background(), p, applyTuning, false, progress)
		})
	default:
		profileName := values["profile"]
		applyTuning := yes(values["apply_tuning"])
		m.mode = modeRunning
		return m, tea.Batch(m.spinner.Tick, runCmd(func() (string, error) {
			return m.runProfileAction(context.Background(), m.formAction, profileName, applyTuning)
		}))
	}
}

func (m Model) runProfileAction(ctx context.Context, act action, profileName string, applyTuning bool) (string, error) {
	switch act {
	case actionShow:
		return m.controller.Show(profileName)
	case actionOutbound:
		return m.controller.Outbound(profileName)
	case actionTest:
		return m.controller.Test(ctx, profileName)
	case actionDebug:
		return m.controller.Debug(ctx, profileName)
	case actionGenerateLog:
		return m.controller.GenerateLog(ctx, profileName)
	case actionStatus:
		return m.controller.Status(ctx, profileName)
	case actionStart:
		return m.controller.Manage(ctx, "start", profileName)
	case actionStop:
		return m.controller.Manage(ctx, "stop", profileName)
	case actionRestart:
		return m.controller.Manage(ctx, "restart", profileName)
	case actionEnable:
		return m.controller.Manage(ctx, "enable", profileName)
	case actionDisable:
		return m.controller.Manage(ctx, "disable", profileName)
	case actionTune:
		return m.controller.Tune(ctx, profileName)
	case actionRescue:
		return m.controller.Resume(ctx, profileName, applyTuning)
	case actionDelete:
		return m.controller.Delete(ctx, profileName)
	default:
		return "", fmt.Errorf("unsupported action")
	}
}

func (m Model) startProgress(title string, fn func(ops.ProgressFunc) (string, error)) (tea.Model, tea.Cmd) {
	ch := make(chan tea.Msg)
	m.mode = modeRunning
	m.progress = []ops.ProgressEvent{{Step: title, Status: ops.StepInfo}}
	m.progressCh = ch
	go func() {
		output, err := fn(func(event ops.ProgressEvent) {
			ch <- progressMsg{event: event}
		})
		ch <- progressDoneMsg{output: output, err: err}
		close(ch)
	}()
	return m, tea.Batch(m.spinner.Tick, waitProgressCmd(ch))
}

func waitProgressCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return progressDoneMsg{}
		}
		return msg
	}
}

func (m *Model) upsertProgress(event ops.ProgressEvent) {
	for i := range m.progress {
		if m.progress[i].Step == event.Step {
			m.progress[i] = event
			return
		}
	}
	m.progress = append(m.progress, event)
}

func (m Model) View() string {
	switch m.mode {
	case modeMenu:
		return m.viewMenu()
	case modeForm, modeProfilePrompt:
		return m.viewForm()
	case modeRunning:
		return m.viewRunning()
	case modeOutput:
		header := titleStyle.Render("XCT Controller")
		if m.err != nil {
			header += " " + errorStyle.Render("completed with errors")
		}
		return header + "\n\n" + m.viewport.View() + "\n\n" + mutedStyle.Render("esc/q: menu  up/down: scroll")
	default:
		return ""
	}
}

func (m Model) viewRunning() string {
	if len(m.progress) == 0 {
		return boxStyle.Width(contentWidth(m.width)).Render(m.spinner.View() + " working...\n\nLong-running operations can take a moment.")
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Working") + "\n")
	b.WriteString(mutedStyle.Render("Deployment progress is updated live. Failed steps are saved for Rescue.") + "\n\n")
	for _, step := range m.progress {
		icon := "·"
		switch step.Status {
		case ops.StepRunning:
			icon = m.spinner.View()
		case ops.StepDone:
			icon = "✓"
		case ops.StepFailed:
			icon = "✗"
		case ops.StepInfo:
			icon = "i"
		}
		line := fmt.Sprintf("%s %s", icon, step.Step)
		if step.Err != "" {
			line += ": " + step.Err
		} else if step.Detail != "" {
			line += ": " + step.Detail
		}
		b.WriteString(line + "\n")
	}
	return boxStyle.Width(contentWidth(m.width)).Render(b.String())
}

func (m Model) viewMenu() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("XCT Controller") + "\n")
	if m.inProfile {
		b.WriteString(mutedStyle.Render("Profile: "+m.activeProfile) + "\n\n")
	} else {
		b.WriteString(mutedStyle.Render("Build: "+m.buildInfo.String()) + "\n")
		b.WriteString(mutedStyle.Render("Create a tunnel or select a profile") + "\n\n")
	}
	for i, item := range m.menu {
		if item.separator {
			if item.title != "" {
				line := fmt.Sprintf("  %-24s %s", item.title, mutedStyle.Render(item.help))
				b.WriteString(mutedStyle.Render(line) + "\n")
			} else {
				b.WriteString(mutedStyle.Render(strings.Repeat("─", 44)) + "\n")
			}
			continue
		}
		line := fmt.Sprintf("> %-24s %s", item.title, item.help)
		if item.disabled {
			b.WriteString("> " + fmt.Sprintf("%-24s %s", mutedStyle.Render(item.title), mutedStyle.Render(item.help)) + "\n")
		} else if i == m.selected {
			b.WriteString(activeStyle.Render(line) + "\n")
		} else {
			b.WriteString("> " + fmt.Sprintf("%-24s %s", item.title, mutedStyle.Render(item.help)) + "\n")
		}
	}
	b.WriteString("\n" + mutedStyle.Render("enter: select  j/k: move  q: quit"))
	return boxStyle.Width(contentWidth(m.width)).Render(b.String())
}

func (m Model) viewForm() string {
	var b strings.Builder
	if m.formAction == actionProfilePassword {
		b.WriteString(titleStyle.Render("Outer SSH password") + "\n")
		b.WriteString(mutedStyle.Render("Password-auth profiles need the outer VPS password for remote actions after the app restarts.") + "\n\n")
	} else if m.mode == modeProfilePrompt {
		b.WriteString(titleStyle.Render("Profile action") + "\n\n")
	} else if m.formAction == actionEditSettings {
		b.WriteString(titleStyle.Render("Settings") + "\n")
		b.WriteString(mutedStyle.Render("Configure SOCKS5 for GitHub update checks/downloads.") + "\n\n")
	} else {
		b.WriteString(titleStyle.Render("Create profile") + "\n")
		b.WriteString(mutedStyle.Render("Press enter to advance. Password fields may be left blank when key auth is used.") + "\n\n")
	}
	start, end := m.visibleFieldRange()
	if len(m.fields) > 0 {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("Fields %d-%d of %d", start+1, end, len(m.fields))) + "\n\n")
	}
	for i := start; i < end; i++ {
		input := m.inputs[i]
		label := m.fields[i].label
		if i == m.focus {
			label = activeStyle.Render(label)
		}
		b.WriteString(label + "\n")
		if m.fields[i].checkbox {
			b.WriteString("  " + checkboxView(m.fields[i].value) + "\n\n")
		} else if len(m.fields[i].choices) > 0 {
			b.WriteString("  " + choiceView(m.fields[i]) + "\n\n")
		} else {
			b.WriteString(input.View() + "\n\n")
		}
	}
	b.WriteString(mutedStyle.Render("enter: next/submit  up/down: move  space/left/right: change option  ctrl+i: field help  esc: menu"))
	rendered := boxStyle.Width(contentWidth(m.width)).Render(b.String())
	if m.helpOpen {
		rendered += "\n" + m.fieldHelpPopup()
	}
	return rendered
}

func (m Model) visibleFieldRange() (int, int) {
	if len(m.fields) == 0 {
		return 0, 0
	}
	available := m.height - 10
	if available < 6 {
		available = 6
	}
	visible := available / 3
	if visible < 3 {
		visible = 3
	}
	if visible > len(m.fields) {
		visible = len(m.fields)
	}
	start := m.focus - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(m.fields) {
		start = len(m.fields) - visible
	}
	return start, start + visible
}

func runCmd(fn func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		output, err := fn()
		return resultMsg{output: output, err: err}
	}
}

func (m Model) settingsFields() []field {
	cfg, err := m.settingsStore.Load()
	if err != nil {
		cfg = settings.Default()
	}
	return []field{
		{key: "update_socks_enabled", label: "Use SOCKS5 for updates", value: yesNo(cfg.UpdateSOCKSEnabled), checkbox: true},
		{key: "update_socks_host", label: "SOCKS5 host/IP", value: cfg.UpdateSOCKSHost},
		{key: "update_socks_port", label: "SOCKS5 port", value: strconv.Itoa(cfg.UpdateSOCKSPort)},
		{key: "update_socks_user", label: "SOCKS5 username, empty for no auth", value: cfg.UpdateSOCKSUser},
		{key: "update_socks_pass", label: "SOCKS5 password, empty if none", value: cfg.UpdateSOCKSPass, secret: true},
		{key: "include_prerelease", label: "Include commit prereleases", value: yesNo(cfg.IncludePrerelease), checkbox: true},
	}
}

func reverseFields() []field {
	return append(commonFields("reverse"), []field{
		{key: "cdn_port", label: "CDN HTTPS port", value: "2087"},
		{key: "ws_path", label: "WebSocket path", value: "/xct-reverse-demo"},
		{key: "backend_port", label: "Iran local Xray reverse backend port", value: "18191"},
		{key: "iran_socks_listen", label: "Iran SOCKS listen address", value: "127.0.0.1"},
		{key: "iran_socks_port", label: "Iran SOCKS port", value: "20141"},
		{key: "iran_socks_user", label: "Iran SOCKS username", value: "rain"},
		{key: "iran_socks_pass", label: "Iran SOCKS password", value: "2013", secret: true},
		{key: "iran_vless_listen", label: "Iran local VLESS listen address", value: "127.0.0.1"},
		{key: "iran_vless_port", label: "Iran local VLESS port", value: "20142"},
	}...)
}

func directFields() []field {
	return append(commonFields("direct"), []field{
		{key: "cdn_port", label: "CDN HTTPS port", value: "2083"},
		{key: "ws_path", label: "WebSocket path", value: "/xct-direct-demo"},
		{key: "backend_port", label: "Iran local Xray direct backend port", value: "18192"},
		{key: "outer_vless_listen", label: "Outer local VLESS listen address", value: "127.0.0.1"},
		{key: "outer_vless_port", label: "Outer local VLESS port", value: "20151"},
		{key: "outer_socks_listen", label: "Outer local SOCKS listen address", value: "127.0.0.1"},
		{key: "outer_socks_port", label: "Outer local SOCKS port", value: "20152"},
		{key: "outer_socks_user", label: "Outer SOCKS username", value: "rain"},
		{key: "outer_socks_pass", label: "Outer SOCKS password", value: "2013", secret: true},
	}...)
}

func commonFields(kind string) []field {
	return []field{
		{key: "profile", label: "Profile name", value: kind + "-demo"},
		{key: "domain", label: "Iran CDN domain"},
		{key: "ssl_crt", label: "Iran SSL certificate path"},
		{key: "ssl_key", label: "Iran SSL key path"},
		{key: "xray_bin", label: "Iran Xray binary path", value: domain.DefaultXrayBin},
		{key: "ssh_host", label: "Outer SSH host/IP"},
		{key: "ssh_port", label: "Outer SSH port", value: "22"},
		{key: "ssh_user", label: "Outer SSH username", value: "root"},
		{key: "ssh_auth", label: "SSH authentication", value: "password", choices: []string{"password", "key"}},
		{key: "ssh_key", label: "SSH private key path, empty for default"},
		{key: "ssh_password", label: "SSH password, only when auth=password", secret: true},
		{key: "use_update_socks", label: "Use SOCKS5 from settings for SSH", value: "no", checkbox: true},
		{key: "ssh_socks_enabled", label: "Use SOCKS proxy for SSH", value: "no", checkbox: true},
		{key: "ssh_socks_host", label: "SOCKS host/IP for SSH"},
		{key: "ssh_socks_port", label: "SOCKS port for SSH", value: "20130"},
		{key: "ssh_socks_user", label: "SOCKS username, empty for no auth"},
		{key: "ssh_socks_pass", label: "SOCKS password, empty if none", secret: true},
		{key: "remote_root_mode", label: "Outer privilege mode", value: "root", choices: []string{"root", "sudo"}},
		{key: "apply_tuning", label: "Apply OS network tuning now", value: "yes", checkbox: true},
	}
}

func buildProfile(values map[string]string, act action) (domain.Profile, bool, error) {
	p := domain.Profile{
		Profile:         values["profile"],
		Domain:          values["domain"],
		CDNPort:         mustPort(values["cdn_port"]),
		WSPath:          values["ws_path"],
		SSLCrt:          values["ssl_crt"],
		SSLKey:          values["ssl_key"],
		XrayBin:         valueOr(values["xray_bin"], domain.DefaultXrayBin),
		RemoteXrayBin:   domain.DefaultRemoteXrayBin,
		BackendPort:     mustPort(values["backend_port"]),
		RemoteUUID:      newUUID(),
		SSHHost:         values["ssh_host"],
		SSHPort:         mustPort(values["ssh_port"]),
		SSHUser:         valueOr(values["ssh_user"], "root"),
		SSHAuth:         domain.SSHAuth(valueOr(values["ssh_auth"], "key")),
		SSHKey:          values["ssh_key"],
		SSHSOCKSEnabled: yes(values["ssh_socks_enabled"]),
		SSHSOCKSHost:    values["ssh_socks_host"],
		SSHSOCKSPort:    mustPort(values["ssh_socks_port"]),
		SSHSOCKSUser:    values["ssh_socks_user"],
		SSHSOCKSPass:    values["ssh_socks_pass"],
		RemoteRootMode:  valueOr(values["remote_root_mode"], "root"),
	}
	if act == actionCreateReverse {
		p.Type = domain.Reverse
		p.IranSocksListen = valueOr(values["iran_socks_listen"], "127.0.0.1")
		p.IranSocksPort = mustPort(values["iran_socks_port"])
		p.IranSocksUser = valueOr(values["iran_socks_user"], "rain")
		p.IranSocksPass = valueOr(values["iran_socks_pass"], "2013")
		p.IranLocalVLESSListen = valueOr(values["iran_vless_listen"], "127.0.0.1")
		p.IranLocalVLESSPort = mustPort(values["iran_vless_port"])
		p.IranLocalVLESSUUID = newUUID()
		p.LocalUUID = p.IranLocalVLESSUUID
	} else {
		p.Type = domain.Direct
		p.OuterLocalVLESSListen = valueOr(values["outer_vless_listen"], "127.0.0.1")
		p.OuterLocalVLESSPort = mustPort(values["outer_vless_port"])
		p.OuterLocalVLESSUUID = newUUID()
		p.OuterSocksListen = valueOr(values["outer_socks_listen"], "127.0.0.1")
		p.OuterSocksPort = mustPort(values["outer_socks_port"])
		p.OuterSocksUser = valueOr(values["outer_socks_user"], "rain")
		p.OuterSocksPass = valueOr(values["outer_socks_pass"], "2013")
	}
	p.FillDerivedPaths(domain.DefaultBaseDir)
	if p.SSHAuth != domain.SSHKey && p.SSHAuth != domain.SSHPassword {
		return p, false, fmt.Errorf("ssh_auth must be key or password")
	}
	return p, yes(values["apply_tuning"]), p.Validate()
}

func mustPort(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func yes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "true", "1":
		return true
	default:
		return false
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func settingsFromValues(values map[string]string) settings.Settings {
	cfg := settings.Default()
	cfg.UpdateSOCKSEnabled = yes(values["update_socks_enabled"])
	cfg.UpdateSOCKSHost = values["update_socks_host"]
	cfg.UpdateSOCKSPort = mustPort(values["update_socks_port"])
	if cfg.UpdateSOCKSPort == 0 {
		cfg.UpdateSOCKSPort = settings.Default().UpdateSOCKSPort
	}
	cfg.UpdateSOCKSUser = values["update_socks_user"]
	cfg.UpdateSOCKSPass = values["update_socks_pass"]
	cfg.IncludePrerelease = yes(values["include_prerelease"])
	return cfg
}

func toggleValue(value string) string {
	if yes(value) {
		return "no"
	}
	return "yes"
}

func checkboxView(value string) string {
	if yes(value) {
		return "[x]"
	}
	return "[ ]"
}

func (m Model) fieldHelpPopup() string {
	if len(m.fields) == 0 || m.focus < 0 || m.focus >= len(m.fields) {
		return ""
	}
	f := m.fields[m.focus]
	var b strings.Builder
	b.WriteString(titleStyle.Render(f.label) + "\n\n")
	b.WriteString(fieldHelp(f.key) + "\n")
	if len(f.choices) > 0 {
		b.WriteString("\nOptions: " + strings.Join(f.choices, " / ") + "\n")
	}
	if f.checkbox {
		b.WriteString("\nUse space to toggle this field.\n")
	}
	b.WriteString("\n" + mutedStyle.Render("ctrl+i, enter, esc, or q: close"))
	return boxStyle.Width(contentWidth(m.width)).Render(b.String())
}

func fieldHelp(key string) string {
	helps := map[string]string{
		"profile":              "A short unique name for this tunnel profile. Use only letters, numbers, dash, or underscore. It is used in generated service names, config paths, and x-ui outbound tags.",
		"domain":               "The CDN-enabled domain that points to the Iran VPS, for example sky-01.example.com. nginx on Iran will serve the WebSocket endpoint for this domain.",
		"ssl_crt":              "Path on the Iran VPS to the TLS certificate file for the CDN domain. nginx uses this certificate for the public HTTPS/WebSocket listener.",
		"ssl_key":              "Path on the Iran VPS to the private key that matches the TLS certificate. The file must already exist and be readable by nginx/root.",
		"xray_bin":             "Path to the Xray binary on the Iran VPS. If you use x-ui, the default /usr/local/x-ui/bin/xray-linux-amd64 is usually correct.",
		"ssh_host":             "IP address or hostname of the outer VPS. The controller uses SSH to install prerequisites, upload Xray configs, and manage systemd services.",
		"ssh_port":             "SSH port on the outer VPS. Use 22 unless your server is configured with a custom SSH port.",
		"ssh_user":             "SSH username for the outer VPS. Use root for a fresh VPS when possible; otherwise choose a user with passwordless sudo.",
		"ssh_auth":             "How the controller authenticates to the outer VPS. Choose key for SSH key/agent authentication, or password to use sshpass with the password field.",
		"ssh_key":              "Optional private key path for SSH key authentication. Leave empty to let SSH use its default keys or agent.",
		"ssh_password":         "Password for the outer SSH user. This is only used when SSH authentication is set to password.",
		"use_update_socks":     "Copy the SOCKS5 proxy configured in Settings into the SSH-to-outer fields for this profile. Useful when GitHub and SSH both need the same proxy.",
		"ssh_socks_enabled":    "Enable this when the Iran VPS cannot reach the outer VPS directly and SSH must be routed through a SOCKS5 proxy.",
		"ssh_socks_host":       "SOCKS5 proxy host or IP used only for SSH from Iran to the outer VPS. Leave empty when SOCKS proxy is disabled.",
		"ssh_socks_port":       "SOCKS5 proxy port used only for SSH from Iran to the outer VPS.",
		"ssh_socks_user":       "Optional SOCKS5 username. Leave empty for a no-auth SOCKS proxy.",
		"ssh_socks_pass":       "Optional SOCKS5 password. If set, the controller uses ncat for authenticated SOCKS ProxyCommand.",
		"remote_root_mode":     "How commands run on the outer VPS. Choose root when SSH logs in as root; choose sudo when SSH logs in as a non-root user with passwordless sudo.",
		"apply_tuning":         "Apply TCP tuning on both servers after the profile is deployed. This enables BBR when available and adjusts buffers/backlogs for long-lived tunnels.",
		"cdn_port":             "Public HTTPS-style port on the Iran CDN domain for this profile. Use a CDN-supported port that is not already used by another service.",
		"ws_path":              "WebSocket path for this profile, such as /xct-reverse-demo. Use a unique path per profile to avoid conflicts.",
		"backend_port":         "Local Iran Xray WebSocket backend port. nginx proxies the public WebSocket path to 127.0.0.1 on this port.",
		"iran_socks_listen":    "Listen address for the Iran local SOCKS inbound used by reverse profiles. 127.0.0.1 keeps it local-only.",
		"iran_socks_port":      "Local SOCKS port on Iran for reverse profiles. Apps or x-ui can use this to send traffic through the outer exit.",
		"iran_socks_user":      "Username for the Iran local SOCKS inbound.",
		"iran_socks_pass":      "Password for the Iran local SOCKS inbound.",
		"iran_vless_listen":    "Listen address for the Iran local VLESS inbound used by x-ui in reverse profiles. 127.0.0.1 keeps it local-only.",
		"iran_vless_port":      "Local VLESS port on Iran for reverse profiles. Prefer this over SOCKS when using x-ui outbounds.",
		"outer_vless_listen":   "Listen address for the outer local VLESS inbound used by x-ui in direct profiles. 127.0.0.1 keeps it local-only.",
		"outer_vless_port":     "Local VLESS port on the outer VPS for direct profiles. x-ui on the outer server can use this as an outbound to exit from Iran.",
		"outer_socks_listen":   "Listen address for the outer local SOCKS inbound used for direct profile tests or local apps.",
		"outer_socks_port":     "Local SOCKS port on the outer VPS for direct profile tests or local apps.",
		"outer_socks_user":     "Username for the outer local SOCKS inbound.",
		"outer_socks_pass":     "Password for the outer local SOCKS inbound.",
		"update_socks_enabled": "Enable this when GitHub is not directly reachable and update checks/downloads must go through a SOCKS5 proxy.",
		"update_socks_host":    "SOCKS5 proxy host or IP used by the Update menu item for GitHub API and release asset downloads.",
		"update_socks_port":    "SOCKS5 proxy port used by the Update menu item.",
		"update_socks_user":    "Optional SOCKS5 username for update traffic. Leave empty when the proxy has no authentication.",
		"update_socks_pass":    "Optional SOCKS5 password for update traffic.",
		"include_prerelease":   "Include commit-based prerelease builds created from pushes to main. Disable this to update only to tagged releases.",
	}
	if help, ok := helps[key]; ok {
		return help
	}
	return "No help is available for this field yet."
}

func nextChoice(f field) string {
	return shiftChoice(f, 1)
}

func prevChoice(f field) string {
	return shiftChoice(f, -1)
}

func shiftChoice(f field, delta int) string {
	if len(f.choices) == 0 {
		return f.value
	}
	index := 0
	for i, choice := range f.choices {
		if choice == f.value {
			index = i
			break
		}
	}
	index = (index + delta + len(f.choices)) % len(f.choices)
	return f.choices[index]
}

func choiceView(f field) string {
	parts := make([]string, 0, len(f.choices))
	for _, choice := range f.choices {
		if choice == f.value {
			parts = append(parts, "["+choice+"]")
		} else {
			parts = append(parts, choice)
		}
	}
	return strings.Join(parts, " / ")
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func contentWidth(width int) int {
	if width < 60 {
		return 60
	}
	return width - 6
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
