package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"
)

// ======================================================================================
// THEME & STYLES
// ======================================================================================

var (
	// Colors Palette
	colPrimary   = lipgloss.Color("#25A065") // Phoenix Green
	colSecondary = lipgloss.Color("#006E53") // Darker Green
	colAccent    = lipgloss.Color("#EFB335") // Warning/Accent Yellow
	colText      = lipgloss.Color("#FAFAFA") // White-ish
	colSubText   = lipgloss.Color("#8A8A8A") // Grey
	colFaint     = lipgloss.Color("#4A4A4A") // Dark Grey
	colMidText   = lipgloss.Color("#C4C4C4") // Soft foreground
	colError     = lipgloss.Color("#FF453A") // Red
	colGit       = lipgloss.Color("#F05133") // Git Orange
	colChipBg    = lipgloss.Color("#333333") // Key chip background

	// Base Styles
	docStyle = lipgloss.NewStyle().Margin(1, 2)

	// Text Styles
	subTextStyle = lipgloss.NewStyle().Foreground(colSubText)
	midTextStyle = lipgloss.NewStyle().Foreground(colMidText)

	// List Styles
	titleStyle = lipgloss.NewStyle().
			Foreground(colText).
			Background(colSecondary).
			Padding(0, 1).
			Bold(true)

	itemTitleStyle = lipgloss.NewStyle().
			Foreground(colMidText)

	itemDescStyle = lipgloss.NewStyle().
			Foreground(colFaint)

	// Selected Item
	cursorStyle = lipgloss.NewStyle().
			Foreground(colPrimary).
			Bold(true)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(colPrimary).
				Bold(true)

	// Badges Styles
	verBadgeStyle = lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(colAccent)

	gitBadgeStyle = lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Bold(true).
			Foreground(colText).
			Background(colGit)

	typeBadgeStyle = lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Bold(true).
			Foreground(colText).
			Background(colSecondary)

	// Key chips ([key] label)
	keyCapStyle = lipgloss.NewStyle().
			Foreground(colText).
			Background(colChipBg).
			Padding(0, 1).
			Bold(true)

	// Box/Panel Styles
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colPrimary).
			Padding(1, 2)

	focusedInputStyle = lipgloss.NewStyle().
				Foreground(colPrimary)

	// Header / Footer
	footerStyle = lipgloss.NewStyle().
			Foreground(colSubText)

	updateHintStyle = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	emptyStyle = lipgloss.NewStyle().
			Foreground(colSubText).
			Italic(true).
			Align(lipgloss.Center)

	// Separators / panels
	separatorStyle = lipgloss.NewStyle().
			Foreground(colFaint)

	// Launch steps
	stepDoneStyle    = lipgloss.NewStyle().Foreground(colPrimary)
	stepPendStyle    = lipgloss.NewStyle().Foreground(colFaint)
	stepCurrentStyle = lipgloss.NewStyle().Foreground(colText).Bold(true)

	// Scan status
	scanTextStyle = lipgloss.NewStyle().Foreground(colSubText)

	// Progress text
	progressTextStyle = lipgloss.NewStyle().Foreground(colSubText)

	// Error
	crossStyle = lipgloss.NewStyle().Foreground(colError).Bold(true)
)

// Spring animation tuning. A critically-damped spring (damping ~1) reaches the
// target quickly without oscillating; a slightly under-damped spring gives a
// subtle overshoot for a more lively feel.
const (
	animFPS         = 60
	animFrequency   = 10.0
	animDamping     = 0.65
	animEpsilon     = 0.001
	animFramePeriod = time.Second / animFPS
)

// gradientTitle is the product name rendered with a green-to-amber ramp,
// computed once since the text never changes.
var gradientTitle = gradientText("LazyPLCNext", "#25A065", "#EFB335")

// gradientText renders s with a per-rune color ramp from one hex color to another.
func gradientText(s, from, to string) string {
	runes := []rune(s)
	if len(runes) <= 1 {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(from)).Render(s)
	}
	fr, fg, fb := hexRGB(from)
	tr, tg, tb := hexRGB(to)
	var b strings.Builder
	for i, r := range runes {
		t := float64(i) / float64(len(runes)-1)
		hex := fmt.Sprintf("#%02x%02x%02x",
			lerp(fr, tr, t), lerp(fg, tg, t), lerp(fb, tb, t))
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hex)).Render(string(r)))
	}
	return b.String()
}

func hexRGB(hex string) (r, g, b int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 128, 128, 128
	}
	v, err := strconv.ParseInt(hex, 16, 32)
	if err != nil {
		return 128, 128, 128
	}
	return int(v>>16) & 0xFF, int(v>>8) & 0xFF, int(v & 0xFF)
}

func lerp(a, b int, t float64) int {
	return a + int(math.Round(float64(b-a)*t))
}

// keyChips renders a row of "[key] label" hints in a modern installer style.
func keyChips(pairs ...[2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, keyCapStyle.Render(p[0])+subTextStyle.Render(" "+p[1]))
	}
	return strings.Join(parts, "   ")
}

func humanBytes(n int64) string {
	const kb = 1 << 10
	const mb = 1 << 20
	switch {
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// ======================================================================================
// UI: CUSTOM LIST DELEGATE
// ======================================================================================

type projectDelegate struct {
	UseNerdFonts bool
}

func (d projectDelegate) Height() int                             { return 2 }
func (d projectDelegate) Spacing() int                            { return 0 }
func (d projectDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d projectDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	p, ok := listItem.(ProjectInfo)
	if !ok {
		return
	}

	icon := "📦"
	typeLabel := "PCWEX"
	switch p.Type {
	case TypeFlat:
		icon = "📂"
		typeLabel = "DIR"
	case TypePCWEF:
		icon = "🔗"
		typeLabel = "PCWEF"
	}

	verBadge := verBadgeStyle.Render(fmt.Sprintf("v%s", p.Version))
	typeBadge := typeBadgeStyle.Render(typeLabel)

	var gitBadge string
	if p.GitBranch != "" {
		bName := p.GitBranch
		if len(bName) > 15 {
			bName = bName[:12] + "..."
		}
		gitIcon := ""
		if d.UseNerdFonts {
			gitIcon = "\ue0a0 "
		}
		gitBadge = gitBadgeStyle.Render(gitIcon + bName)
	}

	displayPath := p.Path
	if len(displayPath) > 60 {
		displayPath = "..." + displayPath[len(displayPath)-57:]
	}

	badges := lipgloss.JoinHorizontal(lipgloss.Center, typeBadge, gitBadge, verBadge)

	var line1, line2 string
	if index == m.Index() {
		line1 = cursorStyle.Render("❯") + " " + selectedItemStyle.Render(icon+" "+p.Name)
		line2 = "  " + badges + " " + midTextStyle.Render(displayPath)
	} else {
		line1 = "  " + itemTitleStyle.Render(icon+" "+p.Name)
		line2 = "  " + badges + " " + itemDescStyle.Render(displayPath)
	}

	_, _ = fmt.Fprint(w, line1+"\n"+line2) //nolint:errcheck // rendering to the bubbletea renderer is best-effort
}

// ======================================================================================
// TEA MODEL
// ======================================================================================

type AppState int

const (
	StateConfig AppState = iota
	StateList
	StateLaunching
	StateSuccess
	StateError
	StateUpdateFound
	StateUpdating
)

type model struct {
	state       AppState
	config      Config
	list        list.Model
	textInput   textinput.Model
	spinner     spinner.Model
	logMsg      string
	selectedPrj ProjectInfo
	err         error
	width       int
	height      int
	updateVer   string
	updateURL   string
	updateAvail bool
	directMode  bool // true when launched with a CLI path argument — list is never initialized

	// events funnels background work (scan, launch, update download) into the
	// update loop. A single listener is armed in Init and re-armed after every
	// message received from the channel.
	events chan tea.Msg

	// scan state
	scanning  bool
	scanID    int
	scanItems int
	scanFound int
	scanDur   time.Duration

	// update download state
	updateProg       progress.Model
	updateDownloaded int64
	updateTotal      int64

	// launch steps
	launchStep int

	// Spring animation for dialog reveal (0 = hidden, 1 = fully shown).
	spring    harmonica.Spring
	animPos   float64
	animVel   float64
	animating bool
}

func initialModel(directProj *ProjectInfo) model {
	ti := textinput.New()
	ti.Placeholder = "C:\\PhoenixProjects"
	ti.Prompt = "❯ "
	ti.PromptStyle = focusedInputStyle
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colPrimary)

	pr := progress.New(progress.WithScaledGradient("#25A065", "#EFB335"))
	pr.Width = 40

	m := model{
		state:      StateConfig,
		textInput:  ti,
		spinner:    sp,
		updateProg: pr,
		events:     make(chan tea.Msg, 32),
		spring:     harmonica.NewSpring(harmonica.FPS(animFPS), animFrequency, animDamping),
	}

	if directProj != nil {
		m.selectedPrj = *directProj
		m.state = StateLaunching
		m.directMode = true
		m.animPos = 1.0
		return m
	}

	if cfg, err := loadConfig(); err == nil && cfg.WorkDir != "" {
		if _, err := os.Stat(cfg.WorkDir); err == nil {
			m.config = cfg
			m.state = StateList
			m.scanID = 1
			m.scanning = true
		}
	}

	m.list = m.newProjectList(nil)
	return m
}

func (m *model) newProjectList(items []list.Item) list.Model {
	delegate := projectDelegate{UseNerdFonts: m.config.UseNerdFonts}
	l := list.New(items, delegate, 0, 0)
	// The brand header and custom footer replace the built-in chrome.
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(2)
	if m.width > 0 {
		l.SetSize(m.width-6, m.height-6)
	}
	return l
}

func (m *model) buildList(projects []ProjectInfo) {
	slices.SortFunc(projects, compareProjects)

	items := make([]list.Item, len(projects))
	for i, p := range projects {
		items[i] = p
	}

	m.list = m.newProjectList(items)
	m.state = StateList
}

type tickMsg time.Time

type frameMsg time.Time

func frameCmd() tea.Cmd {
	return tea.Tick(animFramePeriod, func(t time.Time) tea.Msg {
		return frameMsg(t)
	})
}

// startAnimation resets the spring to the hidden state and begins a reveal
// animation towards the target. Returns the command that drives the animation.
func (m *model) startAnimation() tea.Cmd {
	m.animPos = 0.0
	m.animVel = 0.0
	m.animating = true
	return frameCmd()
}

type updateCheckMsg struct {
	version string
	url     string
	err     error
}
type updateDoneMsg struct{ err error }

type scanProgressMsg struct {
	id       int
	items    int
	projects int
}

type scanDoneMsg struct {
	id       int
	projects []ProjectInfo
	duration time.Duration
}

type launchStepMsg struct{ step int }

type updateProgressMsg struct {
	downloaded int64
	total      int64
}

// listenEvents blocks until the next background event arrives. Exactly one
// listener is alive at any time: armed in Init and re-armed by the Update case
// of every channel message. Progress senders use non-blocking sends, so a busy
// UI can only ever drop intermediate progress — never terminal results.
func listenEvents(events chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func runScan(events chan<- tea.Msg, id int, dir string) {
	start := time.Now()
	projects := ScanProjectsProgress(dir, func(items, found int) {
		select {
		case events <- scanProgressMsg{id: id, items: items, projects: found}:
		default: // drop intermediate progress if the UI is behind
		}
	})
	events <- scanDoneMsg{id: id, projects: projects, duration: time.Since(start)}
}

func runUpdate(events chan<- tea.Msg, url string) {
	err := doUpdate(url, func(downloaded, total int64) {
		select {
		case events <- updateProgressMsg{downloaded: downloaded, total: total}:
		default:
		}
	})
	events <- updateDoneMsg{err: err}
}

func runLaunch(events chan<- tea.Msg, proj ProjectInfo) {
	notify := func(step int) {
		select {
		case events <- launchStepMsg{step: step}:
		default:
		}
	}
	events <- launchProject(proj, notify)
}

func checkUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		ver, url, err := checkUpdate()
		return updateCheckMsg{version: ver, url: url, err: err}
	}
}

func waitForNextUpdateCheck() tea.Cmd {
	return tea.Tick(UpdateCheckInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		checkUpdateCmd(),
		waitForNextUpdateCheck(),
		m.spinner.Tick,
		listenEvents(m.events),
	}
	switch {
	case m.directMode:
		go runLaunch(m.events, m.selectedPrj)
	case m.config.WorkDir != "":
		go runScan(m.events, m.scanID, m.config.WorkDir)
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		docStyle = docStyle.MaxWidth(m.width).MaxHeight(m.height)
		barWidth := msg.Width - 24
		if barWidth < 20 {
			barWidth = 20
		}
		if barWidth > 44 {
			barWidth = 44
		}
		m.updateProg.Width = barWidth
		if m.state == StateList {
			m.list.SetSize(msg.Width-6, msg.Height-6)
		}

	case tickMsg:
		return m, tea.Batch(checkUpdateCmd(), waitForNextUpdateCheck())

	case frameMsg:
		if m.animating {
			m.animPos, m.animVel = m.spring.Update(m.animPos, m.animVel, 1.0)
			if math.Abs(m.animPos-1.0) < animEpsilon && math.Abs(m.animVel) < animEpsilon {
				m.animPos = 1.0
				m.animVel = 0.0
				m.animating = false
			} else {
				return m, frameCmd()
			}
		}

	case progress.FrameMsg:
		pm, cmd := m.updateProg.Update(msg)
		m.updateProg = pm.(progress.Model)
		return m, cmd

	case updateCheckMsg:
		if msg.err == nil && msg.version != "" {
			m.updateVer = msg.version
			m.updateURL = msg.url
			m.updateAvail = true
			// Only interrupt with the update prompt when not busy.
			if m.state != StateLaunching && m.state != StateUpdating &&
				m.state != StateUpdateFound && m.state != StateConfig && !m.scanning {
				// Don't interrupt while the user is typing a filter.
				if m.state != StateList || m.list.FilterState() != list.Filtering {
					m.state = StateUpdateFound
					return m, m.startAnimation()
				}
			}
		}

	case updateDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = StateError
		} else {
			m.logMsg = "Update successful! Please restart."
			m.state = StateSuccess
		}
		return m, tea.Batch(m.startAnimation(), listenEvents(m.events))

	case scanProgressMsg:
		if msg.id == m.scanID {
			m.scanItems, m.scanFound = msg.items, msg.projects
		}
		return m, listenEvents(m.events)

	case scanDoneMsg:
		if msg.id == m.scanID {
			m.scanning = false
			m.scanDur = msg.duration
			m.buildList(msg.projects)
		}
		return m, listenEvents(m.events)

	case launchStepMsg:
		m.launchStep = msg.step
		return m, listenEvents(m.events)

	case launchResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = StateError
		} else {
			m.logMsg = msg.message
			m.launchStep = len(launchSteps)
			m.state = StateSuccess
		}
		return m, tea.Batch(m.startAnimation(), listenEvents(m.events))

	case updateProgressMsg:
		m.updateDownloaded, m.updateTotal = msg.downloaded, msg.total
		cmd := listenEvents(m.events)
		if msg.total > 0 {
			cmd = tea.Batch(m.updateProg.SetPercent(float64(msg.downloaded)/float64(msg.total)), cmd)
		}
		return m, cmd

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.state != StateUpdating {
				return m, tea.Quit
			}
		}
		if m.state == StateList && msg.String() == "q" && m.list.FilterState() != list.Filtering {
			return m, tea.Quit
		}

		if m.state == StateSuccess {
			if strings.Contains(m.logMsg, "Update successful") && (msg.String() == "r" || msg.String() == "R") {
				restartApp()
				return m, tea.Quit
			}
			switch msg.String() {
			case "esc", "enter", "q":
				if m.directMode {
					return m, tea.Quit
				}
				m.state = StateList
				return m, nil
			}
		}
	}

	switch m.state {
	case StateUpdateFound:
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "y", "Y", "enter":
				m.state = StateUpdating
				m.updateDownloaded, m.updateTotal = 0, 0
				go runUpdate(m.events, m.updateURL)
				return m, tea.Batch(m.startAnimation(), m.spinner.Tick)
			case "n", "N", "esc":
				if m.directMode {
					return m, tea.Quit
				}
				m.state = StateList
				return m, nil
			}
		}
		return m, nil

	case StateUpdating:
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		return m, spinCmd

	case StateConfig:
		if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyEsc {
			if m.config.WorkDir != "" {
				m.state = StateList
				return m, nil
			}
			return m, tea.Quit
		}

		var tiCmd tea.Cmd
		m.textInput, tiCmd = m.textInput.Update(msg)
		if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyEnter {
			path := strings.TrimSpace(m.textInput.Value())
			if path != "" {
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					m.config.WorkDir = path
					if err := saveConfig(m.config); err != nil {
						WriteLog("Failed to save config: " + err.Error())
					}
					m.scanID++
					m.scanning = true
					m.scanItems, m.scanFound = 0, 0
					m.scanDur = 0
					m.state = StateList
					go runScan(m.events, m.scanID, path)
					return m, m.spinner.Tick
				}
				m.textInput.Placeholder = "Invalid directory! Try again..."
				m.textInput.SetValue("")
			}
		}
		return m, tiCmd

	case StateList:
		if key, ok := msg.(tea.KeyMsg); ok {
			if m.list.FilterState() != list.Filtering {
				if key.String() == "c" {
					m.state = StateConfig
					m.textInput.SetValue(m.config.WorkDir)
					m.textInput.CursorEnd()
					m.textInput.Focus()
					return m, m.startAnimation()
				}
				if !m.scanning && key.Type == tea.KeyEnter {
					if i, ok := m.list.SelectedItem().(ProjectInfo); ok {
						m.selectedPrj = i
						m.state = StateLaunching
						m.launchStep = 0
						go runLaunch(m.events, i)
						return m, tea.Batch(m.startAnimation(), m.spinner.Tick)
					}
				}
			}
		}
		if m.scanning {
			var spinCmd tea.Cmd
			m.spinner, spinCmd = m.spinner.Update(msg)
			return m, spinCmd
		}
		var listCmd tea.Cmd
		m.list, listCmd = m.list.Update(msg)
		return m, listCmd

	case StateLaunching:
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		return m, spinCmd

	case StateError:
		if _, ok := msg.(tea.KeyMsg); ok {
			if m.directMode {
				return m, tea.Quit
			}
			m.state = StateList
			return m, nil
		}
	}

	return m, nil
}

// ======================================================================================
// VIEW
// ======================================================================================

func (m model) View() string {
	centerContent := func(content string) string {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			content)
	}

	dialogWidth := 60
	if m.width > 0 && m.width-8 < dialogWidth {
		dialogWidth = m.width - 8
	}
	if dialogWidth < 20 {
		dialogWidth = 20
	}

	// Spring-driven reveal factor (0..1). Clamped so the box never collapses
	// below a readable size while animating.
	reveal := m.animPos
	if reveal < 0.0 {
		reveal = 0.0
	} else if reveal > 1.0 {
		reveal = 1.0
	}
	animWidth := int(reveal * float64(dialogWidth))
	if animWidth < 20 {
		animWidth = 20
	}

	switch m.state {
	case StateUpdateFound:
		old := subTextStyle.Render("v" + AppVersion)
		arrow := updateHintStyle.Render("  →  ")
		newVer := lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(m.updateVer)
		ui := lipgloss.JoinVertical(lipgloss.Center,
			titleStyle.Render(" UPDATE AVAILABLE "),
			"",
			old+arrow+newVer,
			"",
			subTextStyle.Render("Download and install now?"),
			keyChips([2]string{"y", "install"}, [2]string{"n", "later"}),
		)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateUpdating:
		var line string
		if m.updateTotal > 0 {
			pct := float64(m.updateDownloaded) / float64(m.updateTotal)
			line = fmt.Sprintf("%s / %s  ·  %.0f%%", humanBytes(m.updateDownloaded), humanBytes(m.updateTotal), pct*100)
		} else {
			line = humanBytes(m.updateDownloaded) + " downloaded"
		}
		ui := lipgloss.JoinVertical(lipgloss.Center,
			titleStyle.Render(" UPDATING "),
			"",
			m.spinner.View()+" Downloading LazyPLCNext "+m.updateVer,
			"",
			m.updateProg.View(),
			progressTextStyle.Render(line),
			"",
			subTextStyle.Render("Please wait…"),
		)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateConfig:
		ui := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render(" CONFIGURATION "),
			"",
			midTextStyle.Render("Where are your PLCnext projects?"),
			m.textInput.View(),
			"",
			keyChips([2]string{"enter", "scan"}, [2]string{"esc", "cancel"}),
		)
		box := boxStyle.Width(animWidth)
		return centerContent(box.Render(ui))

	case StateList:
		header := m.renderHeader()
		status := m.renderStatusBar()

		contentWidth := m.width - 4
		if contentWidth < 1 {
			contentWidth = 1
		}

		sep := separatorStyle.Render(strings.Repeat("─", contentWidth))

		var body string
		switch {
		case m.scanning:
			body = lipgloss.JoinVertical(lipgloss.Left,
				m.spinner.View()+" Scanning "+m.config.WorkDir,
				scanTextStyle.Render(fmt.Sprintf("%d items · %d projects found", m.scanItems, m.scanFound)),
			)
		case len(m.list.Items()) == 0:
			body = emptyStyle.Width(contentWidth).Render(
				"No projects found in\n" + m.config.WorkDir + "\n\n" +
					keyChips([2]string{"c", "choose another folder"}))
		default:
			body = m.list.View()
		}

		return docStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			header,
			sep,
			body,
			sep,
			status,
		))

	case StateLaunching:
		info := lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(m.selectedPrj.Name)
		ver := verBadgeStyle.Render("v" + m.selectedPrj.Version)

		branchInfo := ""
		if m.selectedPrj.GitBranch != "" {
			gitIcon := ""
			if m.config.UseNerdFonts {
				gitIcon = "\ue0a0 "
			}
			branchInfo = gitBadgeStyle.Render(gitIcon + m.selectedPrj.GitBranch)
		}

		steps := make([]string, 0, len(launchSteps))
		for i, s := range launchSteps {
			switch {
			case i < m.launchStep:
				steps = append(steps, stepDoneStyle.Render("✔ "+s))
			case i == m.launchStep:
				steps = append(steps, stepCurrentStyle.Render("❯ "+s))
			default:
				steps = append(steps, stepPendStyle.Render("○ "+s))
			}
		}

		lines := append([]string{
			m.spinner.View() + " Launching Environment",
			"",
			info,
			lipgloss.JoinHorizontal(lipgloss.Center, ver, branchInfo),
			"",
		}, steps...)
		ui := lipgloss.JoinVertical(lipgloss.Center, lines...)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateSuccess:
		isUpdate := strings.Contains(m.logMsg, "Update successful")

		var help string
		if isUpdate {
			help = keyChips([2]string{"R", "restart now"})
		} else {
			help = keyChips([2]string{"enter", "back to list"})
		}

		lines := []string{gradientText("SUCCESS", "#25A065", "#EFB335"), ""}
		if m.selectedPrj.Name != "" {
			lines = append(lines, midTextStyle.Bold(true).Render(m.selectedPrj.Name))
		}
		lines = append(lines, subTextStyle.Render(m.logMsg), "", help)
		ui := lipgloss.JoinVertical(lipgloss.Center, lines...)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateError:
		errWidth := animWidth - 4
		if errWidth < 10 {
			errWidth = 10
		}
		ui := lipgloss.JoinVertical(lipgloss.Center,
			crossStyle.Render("✖ ERROR"),
			"",
			lipgloss.NewStyle().Width(errWidth).Align(lipgloss.Center).Render(fmt.Sprintf("%v", m.err)),
			"",
			subTextStyle.Render("Press any key to return"),
		)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))
	}

	return ""
}

func (m model) renderHeader() string {
	contentWidth := m.width - 4
	if contentWidth < 1 {
		contentWidth = 1
	}

	left := lipgloss.JoinHorizontal(lipgloss.Center,
		gradientTitle,
		" ",
		verBadgeStyle.Render("v"+AppVersion),
	)
	if m.updateAvail {
		left = lipgloss.JoinHorizontal(lipgloss.Center, left, updateHintStyle.Render("  ↑ update available"))
	}

	leftLen := lipgloss.Width(left)
	dots := ""
	if contentWidth > leftLen {
		dots = strings.Repeat(" ", contentWidth-leftLen)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, left, dots)
}

func (m model) renderStatusBar() string {
	contentWidth := m.width - 4
	if contentWidth < 1 {
		contentWidth = 1
	}

	var left string
	switch {
	case m.scanning:
		left = footerStyle.Render(fmt.Sprintf("Scanning… %d items · %d found", m.scanItems, m.scanFound))
	case m.scanDur > 0:
		left = footerStyle.Render(fmt.Sprintf("%d projects", len(m.list.Items()))) +
			subTextStyle.Render(fmt.Sprintf(" · scanned in %.1fs", m.scanDur.Seconds()))
	default:
		left = footerStyle.Render(fmt.Sprintf("%d projects", len(m.list.Items())))
	}

	right := keyChips(
		[2]string{"/", "filter"},
		[2]string{"↵", "launch"},
		[2]string{"c", "path"},
		[2]string{"q", "quit"},
	)

	leftLen := lipgloss.Width(left)
	gap := contentWidth - leftLen - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		strings.Repeat(" ", gap),
		right,
	)
}
