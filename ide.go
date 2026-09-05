package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shirou/gopsutil/v4/process"
)

// ======================================================================================
// IDE DISCOVERY & LAUNCH
// ======================================================================================

var (
	ideInstallDirRe = regexp.MustCompile(`PLCnext Engineer (\d+(\.\d+)+)`)
	versionNumRe    = regexp.MustCompile(`(\d+(\.\d+)+)`)
)

func FindInstalledIDEs() map[string]string {
	versions := make(map[string]string)
	entries, err := os.ReadDir(IDEBasePath)
	if err != nil {
		return versions
	}
	exeNames := []string{"PLCNENG64.exe", "PLCnextEngineer.exe"}
	for _, e := range entries {
		if e.IsDir() && ideInstallDirRe.MatchString(e.Name()) {
			matches := ideInstallDirRe.FindStringSubmatch(e.Name())
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
			match := versionNumRe.FindString(dir)
			if match == targetVer {
				return exePath, p.Pid, true
			}
		}
	}
	return "", 0, false
}

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

		absPath, err := filepath.Abs(launchPath)
		if err == nil {
			launchPath = absPath
		}

		installed := FindInstalledIDEs()
		idePath, ok := installed[targetVer]

		if !ok {
			var keys []string
			for k := range installed {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if len(keys) > 0 {
				idePath = installed[keys[len(keys)-1]]
				WriteLog(fmt.Sprintf("Exact version %s not found. Using latest available: %s", targetVer, idePath))
			} else {
				return launchResultMsg{err: fmt.Errorf("no PLCnext Engineer installation found")}
			}
		} else {
			WriteLog(fmt.Sprintf("Found exact IDE match: %s", idePath))
		}

		// Calculate the intended version from the determined IDE path.
		// This handles cases where we fallback to a different version or proj.Version was "Unknown"
		targetDir := filepath.Base(filepath.Dir(idePath))
		intendedVersion := versionNumRe.FindString(targetDir)
		WriteLog("Intended IDE version to run: " + intendedVersion)

		// Check ALL running processes to find conflicts
		procs, _ := process.Processes()
		for _, p := range procs {
			name, err := p.Name()
			if err != nil {
				continue
			}

			// If we find a running PLCnext Engineer process
			if strings.Contains(name, "PLCNENG64") || strings.Contains(name, "PLCnextEngineer") {
				exePath, err := p.Exe()
				if err != nil {
					continue
				}

				// Extract version of the running process
				runningDir := filepath.Base(filepath.Dir(exePath))
				runningVer := versionNumRe.FindString(runningDir)

				if runningVer != "" && runningVer != intendedVersion {
					WriteLog(fmt.Sprintf("CONFLICT: Found running IDE v%s (PID: %d). Intended is v%s. Killing...", runningVer, p.Pid, intendedVersion))
					if err := p.Kill(); err != nil {
						WriteLog(fmt.Sprintf("Warning: Failed to kill process %d: %v", p.Pid, err))
					} else {
						// Wait briefly for the process to actually exit to avoid file lock issues
						time.Sleep(2 * time.Second)
						WriteLog("Old process killed.")
					}
				} else if runningVer == intendedVersion {
					WriteLog(fmt.Sprintf("Same version v%s is already running. Proceeding to attach/open.", runningVer))
				}
			}
		}

		WriteLog(fmt.Sprintf("Executing: %s \"%s\"", idePath, launchPath))
		cmd := exec.Command(idePath, launchPath)
		cmd.Dir = filepath.Dir(idePath)
		if err := cmd.Start(); err != nil {
			WriteLog(fmt.Sprintf("Launch error: %v", err))
			return launchResultMsg{err: err}
		}

		return launchResultMsg{message: fmt.Sprintf("IDE started: %s", filepath.Base(idePath))}
	}
}
