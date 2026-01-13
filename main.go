package main

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/minio/selfupdate"
	"github.com/shirou/gopsutil/v3/process"
)

// ======================================================================================
// CONFIG & CONSTANTS
// ======================================================================================

const (
	ConfigFileName = "launcher_config.json"
	LogFileName    = "plcnext_launcher.log"
	IDEBasePath    = `C:\Program Files\PHOENIX CONTACT`
	RepoOwner      = "suprunchuk"
	RepoName       = "LazyPLCNext"
)

var AppVersion = "dev"

// --- THEME & STYLES ---

var (
	// Colors
	primaryColor   = lipgloss.Color("#25A065") // Phoenix Green
	secondaryColor = lipgloss.Color("#006E53") // Darker Green
	accentColor    = lipgloss.Color("#EFB335") // Warning/Accent Yellow
	textColor      = lipgloss.Color("#FAFAFA")
	subTextColor   = lipgloss.Color("#787878")
	errorColor     = lipgloss.Color("#FF453A")

	// Base Styles
	docStyle = lipgloss.NewStyle().Margin(1, 2)

	// Text Styles
	subTextStyle = lipgloss.NewStyle().Foreground(subTextColor)

	// List Styles
	titleStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Background(secondaryColor).
			Padding(0, 1).
			Bold(true)

	itemTitleStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Bold(true)

	itemDescStyle = lipgloss.NewStyle().
			Foreground(subTextColor)

	selectedItemStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(primaryColor).
				Foreground(primaryColor).
				Padding(0, 0, 0, 1)

	// Box/Panel Styles
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1, 2)

	focusedInputStyle = lipgloss.NewStyle().
				Foreground(primaryColor)
)

// ======================================================================================
// TYPES
// ======================================================================================

type Config struct {
	WorkDirs []string `json:"work_dirs"`
}

type ProjectType int

const (
	TypeUnknown ProjectType = iota
	TypePCWEX               // Archive (.pcwex)
	TypePCWEF               // Launcher file (.pcwef)
	TypeFlat                // Unpacked Folder (Solution.xml without .pcwef)
)

type ProjectInfo struct {
	Name    string
	Path    string
	Type    ProjectType
	Version string
	IsPCWEF bool
}

// Implement list.Item interface
func (p ProjectInfo) FilterValue() string { return p.Name }
func (p ProjectInfo) Title() string       { return p.Name }
func (p ProjectInfo) Description() string { return p.Path }

// ======================================================================================
// AUTO UPDATE LOGIC
// ======================================================================================

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		BrowserDownloadURL string `json:"browser_download_url"`
		Name               string `json:"name"`
	} `json:"assets"`
}

func checkUpdate() (string, string, error) {
	if AppVersion == "dev" {
		return "", "", nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", RepoOwner, RepoName)
	resp, err := http.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github api status: %s", resp.Status)
	}
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}
	if release.TagName != "" && release.TagName != AppVersion {
		for _, asset := range release.Assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") {
				return release.TagName, asset.BrowserDownloadURL, nil
			}
		}
	}
	return "", "", nil
}

func doUpdate(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	err = selfupdate.Apply(resp.Body, selfupdate.Options{})
	if err != nil {
		return err
	}
	return nil
}

// cleanupOldVersion удаляет файл .old, оставшийся после обновления
func cleanupOldVersion() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	oldExe := exe + ".old"
	// Проверяем, существует ли старый файл
	if _, err := os.Stat(oldExe); err == nil {
		// Пытаемся удалить. Ошибки игнорируем (например, если файл все еще занят),
		// удалится при следующем запуске.
		_ = os.Remove(oldExe)
	}
}

// restartApp перезапускает текущее приложение
func restartApp() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		WriteLog(fmt.Sprintf("Failed to restart: %v", err))
		return
	}
	os.Exit(0)
}

// ======================================================================================
// BUSINESS LOGIC (UNCHANGED)
// ======================================================================================

func WriteLog(msg string) {
	temp := os.Getenv("TEMP")
	logPath := filepath.Join(temp, LogFileName)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, msg))
}

func findVersionInXML(r io.Reader) string {
	decoder := xml.NewDecoder(r)
	for {
		t, _ := decoder.Token()
		if t == nil {
			break
		}
		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "Property" {
				var key, val string
				for _, attr := range se.Attr {
					if attr.Name.Local == "Key" {
						key = attr.Value
					}
					if attr.Name.Local == "Value" {
						val = attr.Value
					}
				}
				if key == "ProductVersion" && val != "" {
					return val
				}
			}
		}
	}
	return ""
}

func findVersionRegex(content []byte) string {
	re := regexp.MustCompile(`Key="ProductVersion"[^>]*Value="([^"]+)"`)
	matches := re.FindStringSubmatch(string(content))
	if len(matches) > 1 {
		return matches[1]
	}
	re2 := regexp.MustCompile(`Value="([^"]+)"[^>]*Key="ProductVersion"`)
	matches2 := re2.FindStringSubmatch(string(content))
	if len(matches2) > 1 {
		return matches2[1]
	}
	return ""
}

func extractVersionFromZip(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	for _, f := range r.File {
		if strings.HasSuffix(strings.ToLower(f.Name), "additional.xml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			if ver := findVersionInXML(strings.NewReader(string(content))); ver != "" {
				return ver, nil
			}
			if ver := findVersionRegex(content); ver != "" {
				return ver, nil
			}
		}
	}
	return "", fmt.Errorf("version not found")
}

func extractVersionFromFolder(folderPath string) string {
	candidates := []string{
		filepath.Join(folderPath, "_properties", "additional.xml"),
	}
	contentDir := filepath.Join(folderPath, "content")
	if entries, err := os.ReadDir(contentDir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "StorageProperties") && strings.HasSuffix(e.Name(), ".xml") {
				candidates = append(candidates, filepath.Join(contentDir, e.Name()))
			}
		}
	}
	for _, file := range candidates {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if ver := findVersionInXML(strings.NewReader(string(content))); ver != "" {
			return ver
		}
		if ver := findVersionRegex(content); ver != "" {
			return ver
		}
	}
	return "Unknown"
}

func ScanProjects(root string) []ProjectInfo {
	var projects []ProjectInfo
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if strings.HasPrefix(name, ".") || name == "bin" || name == "obj" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "Solution.xml")); err == nil {
				ver := extractVersionFromFolder(path)
				projects = append(projects, ProjectInfo{
					Name: d.Name(), Path: path, Type: TypeFlat, Version: ver,
				})
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		lowerName := strings.ToLower(name)
		if strings.HasSuffix(lowerName, ".pcwex") {
			ver, _ := extractVersionFromZip(path)
			if ver == "" {
				ver = "Unknown"
			}
			projects = append(projects, ProjectInfo{
				Name: name, Path: path, Type: TypePCWEX, Version: ver,
			})
			return nil
		}
		if strings.HasSuffix(lowerName, ".pcwef") {
			baseName := strings.TrimSuffix(name, filepath.Ext(name))
			flatFolder := filepath.Join(filepath.Dir(path), baseName+"Flat")
			ver := "Unknown"
			if _, err := os.Stat(flatFolder); err == nil {
				ver = extractVersionFromFolder(flatFolder)
			}
			projects = append(projects, ProjectInfo{
				Name: name, Path: path, Type: TypePCWEF, Version: ver, IsPCWEF: true,
			})
			return nil
		}
		return nil
	})
	if err != nil {
		WriteLog(fmt.Sprintf("Scan error: %v", err))
	}
	return projects
}

func FindInstalledIDEs() map[string]string {
	versions := make(map[string]string)
	entries, err := os.ReadDir(IDEBasePath)
	if err != nil {
		return versions
	}
	re := regexp.MustCompile(`PLCnext Engineer (\d+(\.\d+)+)`)
	exeNames := []string{"PLCNENG64.exe", "PLCnextEngineer.exe"}
	for _, e := range entries {
		if e.IsDir() && re.MatchString(e.Name()) {
			matches := re.FindStringSubmatch(e.Name())
			ver := matches[1]
			for _, exe := range exeNames {
				fullExe := filepath.Join(IDEBasePath, e.Name(), exe)
				if _, err := os.Stat(fullExe); err == nil {
					versions[ver] = fullExe
					break
				}
			}
		}
	}
	return versions
}

func GetRunningIDE(targetVer string) (string, int32, bool) {
	procs, _ := process.Processes()
	for _, p := range procs {
		name, _ := p.Name()
		if strings.Contains(name, "PLCNENG64") || strings.Contains(name, "PLCnextEngineer") {
			exePath, _ := p.Exe()
			dir := filepath.Base(filepath.Dir(exePath))
			re := regexp.MustCompile(`(\d+(\.\d+)+)`)
			match := re.FindString(dir)
			if match == targetVer {
				return exePath, p.Pid, true
			}
		}
	}
	return "", 0, false
}

// ======================================================================================
// UI: CUSTOM LIST DELEGATE (IMPROVED)
// ======================================================================================

type projectDelegate struct{}

func (d projectDelegate) Height() int                             { return 2 }
func (d projectDelegate) Spacing() int                            { return 1 }
func (d projectDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d projectDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	p, ok := listItem.(ProjectInfo)
	if !ok {
		return
	}

	// Icons for cleaner look
	icon := "📦" // PCWEX
	typeStr := "ARCHIVE"
	switch p.Type {
	case TypeFlat:
		icon = "📂" // Folder
		typeStr = "FOLDER"
	case TypePCWEF:
		icon = "🔗" // Shortcut
		typeStr = "LINK"
	}

	name := p.Name
	ver := fmt.Sprintf("v%s", p.Version)
	meta := fmt.Sprintf("%s • %s", typeStr, ver)
	path := p.Path

	var (
		titleRes string
		descRes  string
	)

	if index == m.Index() {
		// Selected State
		titleRes = selectedItemStyle.Render(fmt.Sprintf("%s %s", icon, name))
		descRes = selectedItemStyle.Copy().UnsetBorderStyle().Render(fmt.Sprintf("%s | %s", meta, path))
	} else {
		// Normal State
		titleRes = itemTitleStyle.Render(fmt.Sprintf("%s %s", icon, name))
		descRes = itemDescStyle.Render(fmt.Sprintf("%s | %s", meta, path))
	}

	fmt.Fprint(w, titleRes+"\n"+descRes)
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
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "C:\\PhoenixProjects"
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50
	ti.PromptStyle = focusedInputStyle
	ti.TextStyle = focusedInputStyle

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(primaryColor)

	m := model{
		state:     StateConfig,
		textInput: ti,
		spinner:   sp,
	}

	cfg, err := loadConfig()
	if err == nil && len(cfg.WorkDirs) > 0 {
		if _, err := os.Stat(cfg.WorkDirs[0]); err == nil {
			m.config = cfg
			m.state = StateList
			m.reloadList()
		}
	}

	return m
}

func (m *model) reloadList() {
	if len(m.config.WorkDirs) == 0 {
		return
	}
	projects := ScanProjects(m.config.WorkDirs[0])

	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Type == TypeFlat && projects[j].Type != TypeFlat {
			return true
		}
		if projects[i].Type != TypeFlat && projects[j].Type == TypeFlat {
			return false
		}
		return projects[i].Name < projects[j].Name
	})

	items := make([]list.Item, len(projects))
	for i, p := range projects {
		items[i] = p
	}

	// Customize the list appearance
	delegate := projectDelegate{}
	l := list.New(items, delegate, 0, 0)
	l.Title = "PLCnext Projects"
	l.SetShowHelp(false)
	l.Styles.Title = titleStyle
	l.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(4)

	// Status bar / help keys
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "change path")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "launch")),
		}
	}

	m.list = l
	m.state = StateList
	if m.width > 0 {
		m.list.SetSize(m.width, m.height-2) // Reserve space for borders/footer
	}
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

func performUpdateCmd(url string) tea.Cmd {
	return func() tea.Msg {
		err := doUpdate(url)
		return updateDoneMsg{err: err}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, checkUpdateCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		docStyle = docStyle.MaxWidth(m.width).MaxHeight(m.height)
		if m.state == StateList {
			m.list.SetSize(msg.Width-4, msg.Height-4) // Adjust for margins
		}

	case updateCheckMsg:
		if msg.err == nil && msg.version != "" {
			m.updateVer = msg.version
			m.updateURL = msg.url
			m.state = StateUpdateFound
		}

	case updateDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = StateError
		} else {
			m.logMsg = "Update successful! Please restart."
			m.state = StateSuccess
		}

	case tea.KeyMsg:
		// Обработка клавиши R только в состоянии Success
		if m.state == StateSuccess && (msg.String() == "r" || msg.String() == "R") {
			restartApp()
			return m, tea.Quit // На всякий случай, хотя restartApp делает Exit
		}

		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// Allow quitting from list with Q if not filtering
		if m.state == StateList && msg.String() == "q" && m.list.FilterState() != list.Filtering {
			return m, tea.Quit
		}
	}

	switch m.state {
	case StateUpdateFound:
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "y", "Y", "enter":
				m.state = StateUpdating
				return m, tea.Batch(m.spinner.Tick, performUpdateCmd(m.updateURL))
			case "n", "N", "esc":
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
		var tiCmd tea.Cmd
		m.textInput, tiCmd = m.textInput.Update(msg)
		if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyEnter {
			path := strings.TrimSpace(m.textInput.Value())
			if path != "" {
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					m.config = Config{WorkDirs: []string{path}}
					saveConfig(m.config)
					m.reloadList()
					return m, nil
				} else {
					m.textInput.Placeholder = "Invalid directory!"
					m.textInput.SetValue("")
				}
			}
		}
		return m, tiCmd

	case StateList:
		if key, ok := msg.(tea.KeyMsg); ok {
			if m.list.FilterState() != list.Filtering {
				if key.String() == "c" {
					m.state = StateConfig
					m.textInput.SetValue("")
					m.textInput.Focus()
					return m, nil
				}
			}
			if key.Type == tea.KeyEnter && m.list.FilterState() != list.Filtering {
				if i, ok := m.list.SelectedItem().(ProjectInfo); ok {
					m.selectedPrj = i
					m.state = StateLaunching
					return m, tea.Batch(m.spinner.Tick, launchProjectCmd(m.selectedPrj))
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
			} else {
				m.logMsg = res.message
				m.state = StateSuccess
			}
			if m.state == StateSuccess {
				return m, tea.Quit
			}
		}
		return m, spinCmd

	case StateError:
		if key, ok := msg.(tea.KeyMsg); ok {
			if key.Type != tea.KeyNull {
				m.state = StateList
				return m, nil
			}
		}
	}

	return m, cmd
}

// ======================================================================================
// VIEW (THE PRETTY PART)
// ======================================================================================

func (m model) View() string {
	// Center content helper
	centerContent := func(content string) string {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			content)
	}

	switch m.state {
	case StateUpdateFound:
		ui := lipgloss.JoinVertical(lipgloss.Center,
			titleStyle.Render(" UPDATE AVAILABLE "),
			"\n",
			fmt.Sprintf("New version: %s", lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render(m.updateVer)),
			fmt.Sprintf("Current version: %s", AppVersion),
			"\n",
			subTextStyle.Render("Download and install now? (y/n)"),
		)
		return centerContent(boxStyle.Render(ui))

	case StateUpdating:
		ui := lipgloss.JoinVertical(lipgloss.Center,
			m.spinner.View()+" Updating...",
			"\n",
			subTextStyle.Render("Application will restart automatically"),
		)
		return centerContent(boxStyle.Render(ui))

	case StateConfig:
		ui := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render(" CONFIGURATION "),
			"\n",
			lipgloss.NewStyle().Foreground(textColor).Render("Enter project directory path:"),
			m.textInput.View(),
			"\n",
			subTextStyle.Render("Press Enter to scan"),
		)
		return centerContent(boxStyle.Render(ui))

	case StateList:
		// Adding a footer with status
		status := fmt.Sprintf("Ver: %s | Projects: %d | 'c': config | 'q': quit", AppVersion, len(m.list.Items()))
		statusView := lipgloss.NewStyle().
			Foreground(subTextColor).
			Width(m.width - 4).
			Align(lipgloss.Right).
			Render(status)

		return docStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			m.list.View(),
			statusView,
		))

	case StateLaunching:
		info := lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render(m.selectedPrj.Name)
		ver := lipgloss.NewStyle().Foreground(subTextColor).Render("v" + m.selectedPrj.Version)

		ui := lipgloss.JoinVertical(lipgloss.Center,
			m.spinner.View()+" Launching Environment",
			"\n",
			info,
			ver,
			"\n",
			lipgloss.NewStyle().Italic(true).Foreground(subTextColor).Render("Checking processes..."),
		)
		return centerContent(boxStyle.Render(ui))

	case StateSuccess:
		// Check if message is about update or launch
		isUpdate := strings.Contains(m.logMsg, "Update successful")

		var helpText string
		if isUpdate {
			helpText = subTextStyle.Render("Press 'R' to restart now")
		} else {
			helpText = ""
		}

		ui := lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("✔ SUCCESS"),
			"\n",
			m.logMsg,
			"\n",
			helpText,
		)
		return centerContent(boxStyle.Render(ui))

	case StateError:
		ui := lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render("✖ ERROR"),
			"\n",
			lipgloss.NewStyle().Width(50).Align(lipgloss.Center).Render(fmt.Sprintf("%v", m.err)),
			"\n",
			subTextStyle.Render("Press any key to return"),
		)
		return centerContent(boxStyle.Render(ui))
	}

	return ""
}

// ======================================================================================
// LAUNCH COMMANDS (UNCHANGED)
// ======================================================================================

type launchResultMsg struct {
	message string
	err     error
}

func launchProjectCmd(proj ProjectInfo) tea.Cmd {
	return func() tea.Msg {
		WriteLog("---------------------------------------------------------------")
		WriteLog("Starting launch sequence for: " + proj.Name)

		launchPath := proj.Path
		targetVer := proj.Version
		WriteLog("Project version detected: " + targetVer)

		installed := FindInstalledIDEs()
		idePath, ok := installed[targetVer]

		if !ok {
			var keys []string
			for k := range installed {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) > 0 {
				idePath = installed[keys[len(keys)-1]]
				WriteLog(fmt.Sprintf("Exact version %s not found. Using latest available: %s", targetVer, idePath))
			} else {
				return launchResultMsg{err: fmt.Errorf("no PLCnext Engineer installation found")}
			}
		} else {
			WriteLog(fmt.Sprintf("Found exact IDE match: %s", idePath))
		}

		_, pid, isRunning := GetRunningIDE(targetVer)
		if isRunning {
			WriteLog(fmt.Sprintf("Target IDE version is already running (PID: %d).", pid))
		} else {
			procs, _ := process.Processes()
			for _, p := range procs {
				name, _ := p.Name()
				if strings.Contains(name, "PLCNENG64") || strings.Contains(name, "PLCnextEngineer") {
					exe, _ := p.Exe()
					if exe != idePath {
						WriteLog(fmt.Sprintf("Closing mismatching IDE version (PID: %d, Path: %s)", p.Pid, exe))
						p.Kill()
					}
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		WriteLog(fmt.Sprintf("Executing: %s \"%s\"", idePath, launchPath))
		cmd := exec.Command(idePath, launchPath)
		if err := cmd.Start(); err != nil {
			WriteLog(fmt.Sprintf("Launch error: %v", err))
			return launchResultMsg{err: err}
		}

		return launchResultMsg{message: fmt.Sprintf("IDE started: %s", filepath.Base(idePath))}
	}
}

// ======================================================================================
// CONFIG UTILS (UNCHANGED)
// ======================================================================================

func loadConfig() (Config, error) {
	var cfg Config
	exePath, _ := os.Executable()
	configPath := filepath.Join(filepath.Dir(exePath), ConfigFileName)
	file, err := os.Open(configPath)
	if err != nil {
		return cfg, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&cfg)
	return cfg, err
}

func saveConfig(cfg Config) error {
	exePath, _ := os.Executable()
	configPath := filepath.Join(filepath.Dir(exePath), ConfigFileName)
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}

func main() {
	// 1. Попытка удалить старый файл версии (.old)
	cleanupOldVersion()

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
