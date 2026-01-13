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

// AppVersion устанавливается автоматически через -ldflags при сборке в GitHub Actions
// Если запуск локальный, будет значение "dev"
var AppVersion = "dev"

var (
	appStyle = lipgloss.NewStyle().Margin(1, 2)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#25A065")).
			Padding(0, 1)

	statusMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#04B575")).
				Render

	errorMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF0000")).
				Render

	// List Styles
	verStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true) // Phoenix Green-ish
	dirStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5C5C5C"))
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
	// Если версия dev, пропускаем проверку (для локальной разработки)
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

	// Сравниваем версии (простое строковое сравнение)
	// В GitHub Actions мы задаем версию как v1.0.X.
	if release.TagName != "" && release.TagName != AppVersion {
		for _, asset := range release.Assets {
			// Ищем exe файл
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

	// selfupdate делает магию под Windows: переименовывает текущий exe в .old и пишет новый
	err = selfupdate.Apply(resp.Body, selfupdate.Options{})
	if err != nil {
		return err
	}
	return nil
}

// ======================================================================================
// LOGGING & BUSINESS LOGIC
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
// UI: CUSTOM LIST DELEGATE
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

	title := p.Name
	icon := "[?]"
	switch p.Type {
	case TypePCWEX:
		icon = "[ZIP]"
	case TypeFlat:
		icon = "[DIR]"
	case TypePCWEF:
		icon = "[LNK]"
	}

	verStr := fmt.Sprintf("v%s", p.Version)
	desc := p.Path

	if index == m.Index() {
		selectedTitle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("#25A065")).
			Padding(0, 1).
			Render(fmt.Sprintf("%s %s", icon, title))

		selectedVer := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("#25A065")).
			Render(verStr)

		fmt.Fprintf(w, "%s %s\n  %s", selectedTitle, selectedVer, dirStyle.Render(desc))
	} else {
		normalTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(fmt.Sprintf("%s %s", icon, title))
		fmt.Fprintf(w, "%s %s\n  %s", normalTitle, verStyle.Render(verStr), dirStyle.Render(desc))
	}
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
	StateUpdateFound // New State for Update
	StateUpdating    // New State while downloading
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
	updateVer   string // New version found
	updateURL   string // Download URL
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "C:\\PhoenixProjects"
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#25A065"))

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

	l := list.New(items, projectDelegate{}, 0, 0)
	l.Title = fmt.Sprintf("PLCnext Projects (%s)", AppVersion) // Показываем версию в заголовке
	l.SetShowHelp(false)
	l.Styles.Title = titleStyle

	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "change folder")),
		}
	}
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "config")),
		}
	}

	m.list = l
	m.state = StateList
	if m.width > 0 {
		m.list.SetSize(m.width, m.height-2)
	}
}

// Сообщения для обновления
type updateCheckMsg struct {
	version string
	url     string
	err     error
}
type updateDoneMsg struct{ err error }

// Команда проверки обновления
func checkUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		ver, url, err := checkUpdate()
		return updateCheckMsg{version: ver, url: url, err: err}
	}
}

// Команда выполнения обновления
func performUpdateCmd(url string) tea.Cmd {
	return func() tea.Msg {
		err := doUpdate(url)
		return updateDoneMsg{err: err}
	}
}

func (m model) Init() tea.Cmd {
	// При старте мигаем курсором и проверяем обновления
	return tea.Batch(textinput.Blink, checkUpdateCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.state == StateList {
			m.list.SetSize(msg.Width, msg.Height-2)
		}

	case updateCheckMsg:
		// Если нашли новую версию
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
			m.logMsg = "Update successful! Please restart the application."
			m.state = StateSuccess
		}

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.state == StateList && msg.String() == "q" {
			return m, tea.Quit
		}
	}

	switch m.state {
	case StateUpdateFound:
		// Логика окна "Найдено обновление"
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "y", "Y", "enter":
				m.state = StateUpdating
				return m, tea.Batch(m.spinner.Tick, performUpdateCmd(m.updateURL))
			case "n", "N", "esc":
				m.state = StateList // Отказ от обновления, идем в список
				return m, nil
			}
		}
		return m, nil

	case StateUpdating:
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		// Ждем updateDoneMsg
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
					m.textInput.Placeholder = "Path not found or not a directory"
					m.textInput.SetValue("")
				}
			}
		}
		return m, tiCmd

	case StateList:
		if key, ok := msg.(tea.KeyMsg); ok {
			if key.String() == "c" {
				m.state = StateConfig
				m.textInput.SetValue("")
				m.textInput.Focus()
				return m, nil
			}
			if key.Type == tea.KeyEnter {
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

		switch msg := msg.(type) {
		case launchResultMsg:
			if msg.err != nil {
				m.err = msg.err
				m.state = StateError
			} else {
				m.logMsg = msg.message
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

func (m model) View() string {
	switch m.state {
	case StateUpdateFound:
		return appStyle.Render(fmt.Sprintf(
			"%s\n\n"+
				"New version available: %s\n"+
				"Current version: %s\n\n"+
				"%s",
			titleStyle.Render("Update Available"),
			verStyle.Render(m.updateVer),
			m.configVersionView(),
			dirStyle.Render("Do you want to update now? (y/n)"),
		))

	case StateUpdating:
		return appStyle.Render(fmt.Sprintf(
			"\n   %s Downloading update...\n\n   %s",
			m.spinner.View(),
			dirStyle.Render("Please wait, the app will restart automatically (or close)"),
		))

	case StateConfig:
		return appStyle.Render(fmt.Sprintf(
			"%s\n\n"+
				"Enter working directory to scan (recursively):\n\n%s\n\n"+
				"%s",
			titleStyle.Render("PLCnext Launcher Configuration"),
			m.textInput.View(),
			dirStyle.Render("(Press Enter to confirm, Ctrl+C to quit)"),
		))

	case StateList:
		return appStyle.Render(m.list.View())

	case StateLaunching:
		return appStyle.Render(fmt.Sprintf(
			"\n   %s Launching %s...\n\n   %s\n   Path: %s",
			m.spinner.View(),
			verStyle.Render(m.selectedPrj.Name),
			dirStyle.Render("Checking versions and preparing environment..."),
			m.selectedPrj.Path,
		))

	case StateSuccess:
		return appStyle.Render(fmt.Sprintf("\n%s\n\n%s", statusMessageStyle("Done!"), m.logMsg))

	case StateError:
		return appStyle.Render(fmt.Sprintf(
			"\n%s\n\n%v\n\n%s",
			errorMessageStyle("Error Occurred"),
			m.err,
			dirStyle.Render("Press any key to return to list"),
		))
	}

	return ""
}

func (m model) configVersionView() string {
	if AppVersion == "" {
		return "dev"
	}
	return AppVersion
}

// ======================================================================================
// LAUNCH COMMANDS
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
// CONFIG UTILS
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
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
