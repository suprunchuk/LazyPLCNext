package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	// Keep well below the unauthenticated GitHub API limit (60 req/hour per IP).
	UpdateCheckInterval = time.Hour * 6
)

var AppVersion = "dev"

func appVersionLabel() string {
	v := strings.TrimPrefix(strings.TrimSpace(AppVersion), "v")
	v = strings.TrimPrefix(v, "V")
	return "v" + v
}

func main() {
	cleanupOldVersion()

	if len(os.Getenv("DEBUG")) > 0 {
		if f, err := tea.LogToFile("debug.log", "debug"); err == nil {
			defer f.Close()
		}
	}

	// --- CLI argument handling ---
	// Usage: LazyPLCNext.exe [path/to/project.pcwef|.pcwex|folder]
	//        LazyPLCNext.exe --help
	var directProj *ProjectInfo

	args := os.Args[1:]
	for _, arg := range args {
		switch arg {
		case "-h", "--help", "-help":
			fmt.Printf("LazyPLCNext %s\n\n", appVersionLabel())
			fmt.Println("Usage:")
			fmt.Println("  LazyPLCNext.exe                          — open project browser")
			fmt.Println("  LazyPLCNext.exe <path>                   — open project directly")
			fmt.Println()
			fmt.Println("Supported project types:")
			fmt.Println("  *.pcwef   — PLCnext Engineer flat-file project")
			fmt.Println("  *.pcwex   — PLCnext Engineer zipped project")
			fmt.Println("  <folder>  — flat project folder (must contain Solution.xml)")
			fmt.Println()
			fmt.Println("Examples:")
			fmt.Println(`  LazyPLCNext.exe "D:\Projects\MyProject\MyProject.pcwef"`)
			fmt.Println(`  LazyPLCNext.exe "D:\Projects\MyProject\MyProject.pcwex"`)
			fmt.Println(`  LazyPLCNext.exe "D:\Projects\MyProjectFlat"`)
			os.Exit(0)
		default:
			// Treat the first non-flag argument as a project path
			if directProj == nil && !strings.HasPrefix(arg, "-") {
				proj, err := buildProjectInfoFromPath(arg)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					os.Exit(1)
				}
				directProj = &proj
			}
		}
	}

	p := tea.NewProgram(initialModel(directProj), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
