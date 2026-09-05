package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ======================================================================================
// PROJECT TYPES
// ======================================================================================

type ProjectType int

const (
	TypeUnknown ProjectType = iota
	TypePCWEX               // Archive (.pcwex)
	TypePCWEF               // Launcher file (.pcwef)
	TypeFlat                // Unpacked Folder (Solution.xml without .pcwef)
)

type ProjectInfo struct {
	Name      string
	Path      string
	Type      ProjectType
	Version   string
	IsPCWEF   bool
	GitBranch string // New field for Git Branch
}

// Implement list.Item interface
func (p ProjectInfo) FilterValue() string { return p.Name }
func (p ProjectInfo) Title() string       { return p.Name }
func (p ProjectInfo) Description() string { return p.Path }

// ======================================================================================
// VERSION PARSING
// ======================================================================================

var (
	versionKeyFirstRe   = regexp.MustCompile(`Key="ProductVersion"[^>]*Value="([^"]+)"`)
	versionValueFirstRe = regexp.MustCompile(`Value="([^"]+)"[^>]*Key="ProductVersion"`)
)

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
	matches := versionKeyFirstRe.FindStringSubmatch(string(content))
	if len(matches) > 1 {
		return matches[1]
	}
	matches2 := versionValueFirstRe.FindStringSubmatch(string(content))
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
	defer r.Close() //nolint:errcheck // read-only archive: nothing to do on close error

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

// ======================================================================================
// SCANNING
// ======================================================================================

func getGitBranch(startPath string) string {
	dir := startPath
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	runGit := func(d string) string {
		cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		cmd.Dir = d
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			return strings.TrimSpace(out.String())
		}
		return ""
	}

	for i := 0; i < 3; i++ {
		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return runGit(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// compareProjects orders flat projects first, then case-insensitive by name.
func compareProjects(a, b ProjectInfo) int {
	aFlat, bFlat := a.Type == TypeFlat, b.Type == TypeFlat
	if aFlat != bFlat {
		if aFlat {
			return -1
		}
		return 1
	}
	return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
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
				branch := getGitBranch(path)
				projects = append(projects, ProjectInfo{
					Name: d.Name(), Path: path, Type: TypeFlat, Version: ver, GitBranch: branch,
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
			parentDir := filepath.Dir(path)
			branch := getGitBranch(parentDir)
			projects = append(projects, ProjectInfo{
				Name: strings.TrimSuffix(name, filepath.Ext(name)), Path: path, Type: TypePCWEX, Version: ver, GitBranch: branch,
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
			parentDir := filepath.Dir(path)
			branch := getGitBranch(parentDir)
			projects = append(projects, ProjectInfo{
				Name: baseName, Path: path, Type: TypePCWEF, Version: ver, IsPCWEF: true, GitBranch: branch,
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

// buildProjectInfoFromPath constructs a ProjectInfo from a direct file/folder path.
// Supports .pcwex, .pcwef files and flat project folders (containing Solution.xml).
func buildProjectInfoFromPath(rawPath string) (ProjectInfo, error) {
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return ProjectInfo{}, fmt.Errorf("cannot resolve path: %w", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		return ProjectInfo{}, fmt.Errorf("path does not exist: %s", absPath)
	}

	lower := strings.ToLower(absPath)
	parentDir := filepath.Dir(absPath)
	fileName := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))

	switch {
	case strings.HasSuffix(lower, ".pcwex"):
		ver, _ := extractVersionFromZip(absPath)
		if ver == "" {
			ver = "Unknown"
		}
		branch := getGitBranch(parentDir)
		return ProjectInfo{
			Name: fileName, Path: absPath, Type: TypePCWEX, Version: ver, GitBranch: branch,
		}, nil

	case strings.HasSuffix(lower, ".pcwef"):
		baseName := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
		flatFolder := filepath.Join(parentDir, baseName+"Flat")
		ver := "Unknown"
		if _, err := os.Stat(flatFolder); err == nil {
			ver = extractVersionFromFolder(flatFolder)
		}
		branch := getGitBranch(parentDir)
		return ProjectInfo{
			Name: fileName, Path: absPath, Type: TypePCWEF, Version: ver, IsPCWEF: true, GitBranch: branch,
		}, nil

	default:
		// Try flat folder (directory containing Solution.xml)
		if info, err := os.Stat(absPath); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(absPath, "Solution.xml")); err == nil {
				ver := extractVersionFromFolder(absPath)
				branch := getGitBranch(absPath)
				return ProjectInfo{
					Name: filepath.Base(absPath), Path: absPath, Type: TypeFlat, Version: ver, GitBranch: branch,
				}, nil
			}
		}
		return ProjectInfo{}, fmt.Errorf("unsupported project type or not a PLCnext project: %s", rawPath)
	}
}
