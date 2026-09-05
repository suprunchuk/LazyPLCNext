package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const sampleAdditionalXML = `<?xml version="1.0" encoding="utf-8"?>
<Document>
  <Properties>
    <Property Key="ProductVersion" Value="2025.0.0" />
  </Properties>
</Document>
`

func TestFindVersionInXML(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want string
	}{
		{"key before value", `<Property Key="ProductVersion" Value="2025.0.0"/>`, "2025.0.0"},
		{"value before key", `<Property Value="2025.6.2" Key="ProductVersion"/>`, "2025.6.2"},
		{"nested document", sampleAdditionalXML, "2025.0.0"},
		{"other properties ignored", `<Property Key="Author" Value="Phoenix"/><Property Key="ProductVersion" Value="2024.1.0"/>`, "2024.1.0"},
		{"missing version", `<Property Key="Author" Value="Phoenix"/>`, ""},
		{"empty document", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := findVersionInXML(strings.NewReader(tc.xml)); got != tc.want {
				t.Errorf("findVersionInXML() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindVersionRegex(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"key before value", `junk Key="ProductVersion" Value="9.9.9" junk`, "9.9.9"},
		{"value before key", `junk Value="8.1.3" Key="ProductVersion" junk`, "8.1.3"},
		{"missing version", `no version here`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := findVersionRegex([]byte(tc.content)); got != tc.want {
				t.Errorf("findVersionRegex() = %q, want %q", got, tc.want)
			}
		})
	}
}

// writeZip creates a zip archive with the given name->content entries.
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractVersionFromZip(t *testing.T) {
	t.Run("found in additional.xml", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "MyProject.pcwex")
		writeZip(t, path, map[string]string{
			"MyProject/_properties/additional.xml": sampleAdditionalXML,
		})
		ver, err := extractVersionFromZip(path)
		if err != nil || ver != "2025.0.0" {
			t.Errorf("extractVersionFromZip() = %q, %v; want 2025.0.0, nil", ver, err)
		}
	})

	t.Run("regex fallback for non-xml content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "MyProject.pcwex")
		writeZip(t, path, map[string]string{
			"MyProject/_properties/additional.xml": `junk Key="ProductVersion" Value="9.9.9" junk`,
		})
		ver, err := extractVersionFromZip(path)
		if err != nil || ver != "9.9.9" {
			t.Errorf("extractVersionFromZip() = %q, %v; want 9.9.9, nil", ver, err)
		}
	})

	t.Run("version not found", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "MyProject.pcwex")
		writeZip(t, path, map[string]string{
			"MyProject/readme.txt": "no version",
		})
		if _, err := extractVersionFromZip(path); err == nil {
			t.Error("extractVersionFromZip() expected error for zip without version")
		}
	})
}

func TestExtractVersionFromFolder(t *testing.T) {
	t.Run("from _properties/additional.xml", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "_properties"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "_properties", "additional.xml"), []byte(sampleAdditionalXML), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := extractVersionFromFolder(dir); got != "2025.0.0" {
			t.Errorf("extractVersionFromFolder() = %q, want 2025.0.0", got)
		}
	})

	t.Run("from content StorageProperties", func(t *testing.T) {
		dir := t.TempDir()
		contentDir := filepath.Join(dir, "content")
		if err := os.MkdirAll(contentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(contentDir, "StorageProperties_S1.xml"), []byte(sampleAdditionalXML), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := extractVersionFromFolder(dir); got != "2025.0.0" {
			t.Errorf("extractVersionFromFolder() = %q, want 2025.0.0", got)
		}
	})

	t.Run("unknown when no candidates", func(t *testing.T) {
		if got := extractVersionFromFolder(t.TempDir()); got != "Unknown" {
			t.Errorf("extractVersionFromFolder() = %q, want Unknown", got)
		}
	})
}

func TestBuildProjectInfoFromPath(t *testing.T) {
	t.Run("pcwex archive", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "MyProject.pcwex")
		writeZip(t, path, map[string]string{
			"MyProject/_properties/additional.xml": sampleAdditionalXML,
		})
		info, err := buildProjectInfoFromPath(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Type != TypePCWEX || info.Name != "MyProject" || info.Version != "2025.0.0" || info.Path != path {
			t.Errorf("got %+v, want TypePCWEX/MyProject/2025.0.0", info)
		}
	})

	t.Run("pcwef with flat folder", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "MyProject.pcwef"), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
		flat := filepath.Join(dir, "MyProjectFlat", "_properties")
		if err := os.MkdirAll(flat, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(flat, "additional.xml"), []byte(sampleAdditionalXML), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := buildProjectInfoFromPath(filepath.Join(dir, "MyProject.pcwef"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Type != TypePCWEF || !info.IsPCWEF || info.Version != "2025.0.0" {
			t.Errorf("got %+v, want TypePCWEF with version 2025.0.0", info)
		}
	})

	t.Run("pcwef without flat folder", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "MyProject.pcwef")
		if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := buildProjectInfoFromPath(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Type != TypePCWEF || info.Version != "Unknown" {
			t.Errorf("got %+v, want TypePCWEF with Unknown version", info)
		}
	})

	t.Run("flat folder with Solution.xml", func(t *testing.T) {
		dir := t.TempDir()
		props := filepath.Join(dir, "MyProjectFlat", "_properties")
		if err := os.MkdirAll(props, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(props, "additional.xml"), []byte(sampleAdditionalXML), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "MyProjectFlat", "Solution.xml"), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := buildProjectInfoFromPath(filepath.Join(dir, "MyProjectFlat"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Type != TypeFlat || info.Version != "2025.0.0" {
			t.Errorf("got %+v, want TypeFlat with version 2025.0.0", info)
		}
	})

	t.Run("nonexistent path errors", func(t *testing.T) {
		if _, err := buildProjectInfoFromPath(filepath.Join(t.TempDir(), "missing.pcwex")); err == nil {
			t.Error("expected error for nonexistent path")
		}
	})

	t.Run("unsupported file errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "notes.txt")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := buildProjectInfoFromPath(path); err == nil {
			t.Error("expected error for unsupported file type")
		}
	})
}

func TestCompareProjects(t *testing.T) {
	flat := ProjectInfo{Name: "BetaFlat", Type: TypeFlat}
	archive := ProjectInfo{Name: "Alpha", Type: TypePCWEX}
	link := ProjectInfo{Name: "gamma", Type: TypePCWEF}
	flat2 := ProjectInfo{Name: "aaa", Type: TypeFlat}

	sorted := []ProjectInfo{archive, flat, link, flat2}
	slices.SortFunc(sorted, compareProjects)

	want := []ProjectInfo{flat2, flat, archive, link}
	for i := range want {
		if sorted[i] != want[i] {
			t.Errorf("sorted[%d] = %+v, want %+v", i, sorted[i], want[i])
		}
	}
}

func TestScanProjectsProgress(t *testing.T) {
	root := t.TempDir()

	writeZip(t, filepath.Join(root, "Alpha.pcwex"), map[string]string{
		"Alpha/_properties/additional.xml": sampleAdditionalXML,
	})
	beta := filepath.Join(root, "BetaFlat", "_properties")
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beta, "additional.xml"), []byte(sampleAdditionalXML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "BetaFlat", "Solution.xml"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls, lastItems, lastFound int
	projects := ScanProjectsProgress(root, func(items, found int) {
		calls++
		lastItems, lastFound = items, found
	})

	if len(projects) != 2 {
		t.Fatalf("ScanProjectsProgress() returned %d projects, want 2", len(projects))
	}
	if calls < 2 {
		t.Errorf("progress callback fired %d times, want at least one per project", calls)
	}
	if lastFound != 2 {
		t.Errorf("last reported found = %d, want 2", lastFound)
	}
	if lastItems == 0 {
		t.Error("last reported items = 0, want > 0")
	}

	// The plain wrapper must return the same projects without callbacks.
	if plain := ScanProjects(root); len(plain) != len(projects) {
		t.Errorf("ScanProjects() returned %d projects, want %d", len(plain), len(projects))
	}
}

func TestScanProjects(t *testing.T) {
	root := t.TempDir()

	writeZip(t, filepath.Join(root, "Alpha.pcwex"), map[string]string{
		"Alpha/_properties/additional.xml": sampleAdditionalXML,
	})

	beta := filepath.Join(root, "BetaFlat", "_properties")
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beta, "additional.xml"), []byte(sampleAdditionalXML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "BetaFlat", "Solution.xml"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "Gamma.pcwef"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, skipped := range []string{"bin", "obj", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, skipped), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, skipped, "Skip.pcwex"), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := ScanProjects(root)
	if len(got) != 3 {
		t.Fatalf("ScanProjects() returned %d projects (%v), want 3", len(got), got)
	}

	// ScanProjects returns walk order (lexical); sorting is applied separately.
	wantOrder := []struct {
		name string
		typ  ProjectType
		ver  string
	}{
		{"Alpha", TypePCWEX, "2025.0.0"},
		{"BetaFlat", TypeFlat, "2025.0.0"},
		{"Gamma", TypePCWEF, "Unknown"},
	}
	for i, w := range wantOrder {
		if got[i].Name != w.name || got[i].Type != w.typ || got[i].Version != w.ver {
			t.Errorf("projects[%d] = %+v, want name=%s type=%v version=%s", i, got[i], w.name, w.typ, w.ver)
		}
	}
}
