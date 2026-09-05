package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
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
	colSubText   = lipgloss.Color("#6E6E6E") // Grey
	colError     = lipgloss.Color("#FF453A") // Red
	colGit       = lipgloss.Color("#F05133") // Git Orange
	colPath      = lipgloss.Color("#4A4A4A") // Dark Grey for paths

	// Base Styles
	docStyle = lipgloss.NewStyle().Margin(1, 2)

	// Text Styles
	subTextStyle = lipgloss.NewStyle().Foreground(colSubText)

	// List Styles
	titleStyle = lipgloss.NewStyle().
			Foreground(colText).
			Background(colSecondary).
			Padding(0, 1).
			Bold(true)

	// Item Styles
	itemTitleStyle = lipgloss.NewStyle().
			Foreground(colText).
			Bold(true)

	itemDescStyle = lipgloss.NewStyle().
			Foreground(colPath)

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

	// Selected Item
	selectedItemStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(colPrimary).
				Foreground(colPrimary).
				Padding(0, 0, 0, 1).
				Bold(true)

	// Box/Panel Styles
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colPrimary).
			Padding(1, 2)

	focusedInputStyle = lipgloss.NewStyle().
				Foreground(colPrimary)

	// Header / Footer
	appTitleStyle = lipgloss.NewStyle().
			Foreground(colPrimary).
			Bold(true)

	appVersionStyle = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

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
			Foreground(colSecondary)
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
			gitIcon = " "
		}
		gitBadge = gitBadgeStyle.Render(gitIcon + bName)
	}

	var (
		titleRes string
		descRes  string
	)

	displayPath := p.Path
	if len(displayPath) > 60 {
		displayPath = "..." + displayPath[len(displayPath)-57:]
	}

	badges := lipgloss.JoinHorizontal(lipgloss.Center, typeBadge, gitBadge, verBadge)

	if index == m.Index() {
		titleRes = selectedItemStyle.Render(fmt.Sprintf("%s %s", icon, p.Name))
		descRes = selectedItemStyle.Render(
			fmt.Sprintf("%s %s", badges, itemDescStyle.Render(displayPath)),
		)
	} else {
		titleRes = itemTitleStyle.Render(fmt.Sprintf("%s %s", icon, p.Name))
		descRes = fmt.Sprintf("  %s %s", badges, itemDescStyle.Render(displayPath))
	}

	fmt.Fprint(w, titleRes+"\n"+descRes) //nolint:errcheck // rendering to the bubbletea renderer is best-effort
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

	// Spring animation for dialog reveal (0 = hidden, 1 = fully shown).
	spring    harmonica.Spring
	animPos   float64
	animVel   float64
	animating bool
}

func initialModel(directProj *ProjectInfo) model {
	ti := textinput.New()
	ti.Placeholder = "C:\\PhoenixProjects"
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50
	ti.PromptStyle = focusedInputStyle
	ti.TextStyle = focusedInputStyle

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colPrimary)

	m := model{
		state:     StateConfig,
		textInput: ti,
		spinner:   sp,
		spring:    harmonica.NewSpring(harmonica.FPS(animFPS), animFrequency, animDamping),
	}

	if directProj != nil {
		m.selectedPrj = *directProj
		m.state = StateLaunching
		m.directMode = true
		m.animPos = 1.0
		return m
	}

	cfg, err := loadConfig()
	if err == nil && cfg.WorkDir != "" {
		if _, err := os.Stat(cfg.WorkDir); err == nil {
			m.config = cfg
			m.state = StateList
			m.reloadList()
		}
	}

	return m
}

func (m *model) reloadList() {
	if m.config.WorkDir == "" {
		return
	}
	projects := ScanProjects(m.config.WorkDir)

	slices.SortFunc(projects, compareProjects)

	items := make([]list.Item, len(projects))
	for i, p := range projects {
		items[i] = p
	}

	delegate := projectDelegate{UseNerdFonts: m.config.UseNerdFonts}
	l := list.New(items, delegate, 0, 0)
	l.Title = " PLCnext Projects "
	l.SetShowHelp(true)
	l.Styles.Title = titleStyle
	l.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(2)
	l.Styles.HelpStyle = list.DefaultStyles().HelpStyle.PaddingLeft(2).PaddingBottom(0)

	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "change path")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "launch")),
		}
	}
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "change project directory")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "launch selected project")),
		}
	}

	if len(items) == 0 {
		l.SetStatusBarItemName("project", "projects")
	}

	m.list = l
	m.state = StateList
	if m.width > 0 {
		m.list.SetSize(m.width-6, m.height-6)
	}
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

func performUpdateCmd(url string) tea.Cmd {
	return func() tea.Msg {
		err := doUpdate(url)
		return updateDoneMsg{err: err}
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		checkUpdateCmd(),
		waitForNextUpdateCheck(),
	}
	if m.state == StateLaunching {
		cmds = append(cmds, m.spinner.Tick, launchProjectCmd(m.selectedPrj))
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		docStyle = docStyle.MaxWidth(m.width).MaxHeight(m.height)
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

	case updateCheckMsg:
		if msg.err == nil && msg.version != "" {
			m.updateVer = msg.version
			m.updateURL = msg.url
			m.updateAvail = true
			// Only interrupt with the update prompt when not busy.
			if m.state != StateLaunching && m.state != StateUpdating &&
				m.state != StateUpdateFound && m.state != StateConfig {
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
			return m, m.startAnimation()
		}
		m.logMsg = "Update successful! Please restart."
		m.state = StateSuccess
		return m, m.startAnimation()

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
				return m, tea.Batch(m.startAnimation(), m.spinner.Tick, performUpdateCmd(m.updateURL))
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
					m.reloadList()
					return m, nil
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
					currentPath := ""
					if m.config.WorkDir != "" {
						currentPath = m.config.WorkDir
					}
					m.textInput.SetValue(currentPath)
					m.textInput.CursorEnd()
					m.textInput.Focus()
					return m, m.startAnimation()
				}
				if key.Type == tea.KeyEnter {
					if i, ok := m.list.SelectedItem().(ProjectInfo); ok {
						m.selectedPrj = i
						m.state = StateLaunching
						return m, tea.Batch(m.startAnimation(), m.spinner.Tick, launchProjectCmd(m.selectedPrj))
					}
				}
			}
		}
		var listCmd tea.Cmd
		m.list, listCmd = m.list.Update(msg)
		return m, listCmd

	case StateLaunching:
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		if res, ok := msg.(launchResultMsg); ok {
			if res.err != nil {
				m.err = res.err
				m.state = StateError
				return m, tea.Batch(spinCmd, m.startAnimation())
			}
			m.logMsg = res.message
			m.state = StateSuccess
			return m, tea.Batch(spinCmd, m.startAnimation())
		}
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

	return m, cmd
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
		newVer := lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(m.updateVer)
		ui := lipgloss.JoinVertical(lipgloss.Center,
			titleStyle.Render(" UPDATE AVAILABLE "),
			"",
			fmt.Sprintf("New version:      %s", newVer),
			fmt.Sprintf("Current version:  %s", AppVersion),
			"",
			subTextStyle.Render("Download and install now?"),
			updateHintStyle.Render("[y] yes   [n] later"),
		)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateUpdating:
		ui := lipgloss.JoinVertical(lipgloss.Center,
			m.spinner.View()+" Updating...",
			"",
			subTextStyle.Render("Application will restart automatically"),
		)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateConfig:
		ui := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render(" CONFIGURATION "),
			"",
			lipgloss.NewStyle().Foreground(colText).Render("Enter project directory path:"),
			m.textInput.View(),
			"",
			subTextStyle.Render("Enter  scan   Esc  cancel"),
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

		body := m.list.View()
		if len(m.list.Items()) == 0 {
			emptyMsg := emptyStyle.Width(contentWidth).Render(
				"No projects found in:\n" + m.config.WorkDir +
					"\n\nPress [c] to change the directory.",
			)
			body = emptyMsg
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
				gitIcon = " "
			}
			branchInfo = gitBadgeStyle.Render(gitIcon + m.selectedPrj.GitBranch)
		}

		ui := lipgloss.JoinVertical(lipgloss.Center,
			m.spinner.View()+" Launching Environment",
			"",
			info,
			lipgloss.JoinHorizontal(lipgloss.Center, ver, branchInfo),
			"",
			lipgloss.NewStyle().Italic(true).Foreground(colSubText).Render("Checking processes..."),
		)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateSuccess:
		isUpdate := strings.Contains(m.logMsg, "Update successful")

		var helpText string
		if isUpdate {
			helpText = updateHintStyle.Render("Press [R] to restart now")
		} else {
			helpText = subTextStyle.Render("Press Enter or Esc to return to list")
		}

		ui := lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render("✔ SUCCESS"),
			"",
			lipgloss.NewStyle().Foreground(colText).Bold(true).Render(m.selectedPrj.Name),
			subTextStyle.Render(m.logMsg),
			"",
			helpText,
		)
		box := boxStyle.Width(animWidth).Align(lipgloss.Center)
		return centerContent(box.Render(ui))

	case StateError:
		errWidth := animWidth - 4
		if errWidth < 10 {
			errWidth = 10
		}
		ui := lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(colError).Bold(true).Render("✖ ERROR"),
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

	title := appTitleStyle.Render("LazyPLCNext")
	ver := appVersionStyle.Render("v" + AppVersion)

	left := fmt.Sprintf("%s %s", title, ver)

	updateTag := ""
	if m.updateAvail {
		updateTag = updateHintStyle.Render(" ↑ update available")
	}

	leftLen := lipgloss.Width(left) + lipgloss.Width(updateTag)
	dots := ""
	if contentWidth > leftLen {
		dots = strings.Repeat(" ", contentWidth-leftLen)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, left, updateTag, dots)
}

func (m model) renderStatusBar() string {
	contentWidth := m.width - 4
	if contentWidth < 1 {
		contentWidth = 1
	}

	idx := m.list.Index()
	if idx >= len(m.list.Items()) {
		idx = len(m.list.Items()) - 1
	}
	if idx < 0 {
		idx = 0
	}
	pos := footerStyle.Render(fmt.Sprintf("%d/%d", idx+1, len(m.list.Items())))

	left := footerStyle.Render(fmt.Sprintf("Projects: %d", len(m.list.Items())))

	var rightParts []string
	if m.config.UseNerdFonts {
		rightParts = append(rightParts, " nerd-fonts")
	}
	rightParts = append(rightParts, "c config")
	rightParts = append(rightParts, "q quit")
	right := footerStyle.Render(strings.Join(rightParts, "   "))

	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right) + lipgloss.Width(pos) + 2
	gap := contentWidth - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		left,
		strings.Repeat(" ", gap),
		pos,
		" ",
		right,
	)
}
