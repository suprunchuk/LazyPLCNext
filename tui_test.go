package main

import (
	"errors"
	"strings"
	"testing"
)

func TestViewRendersAllStates(t *testing.T) {
	proj := ProjectInfo{Name: "Alpha", Path: `C:\proj\Alpha.pcwex`, Type: TypePCWEX, Version: "2025.0.0"}

	mk := func(state AppState) model {
		m := initialModel(nil)
		m.width, m.height = 100, 30
		m.config = Config{WorkDir: `C:\proj`}
		m.state = state
		return m
	}

	launching := initialModel(&proj)
	launching.width, launching.height = 100, 30

	listScanning := mk(StateList)
	listScanning.scanning = true

	updating := mk(StateUpdating)
	updating.updateVer = "v1.2.3"
	updating.updateTotal = 1000
	updating.updateDownloaded = 500

	errored := mk(StateError)
	errored.err = errors.New("boom")

	cases := []struct {
		name string
		m    model
	}{
		{"config", mk(StateConfig)},
		{"list-scanning", listScanning},
		{"list-empty", mk(StateList)},
		{"update-found", func() model { m := mk(StateUpdateFound); m.updateVer = "v1.2.3"; return m }()},
		{"updating", updating},
		{"launching", launching},
		{"success", mk(StateSuccess)},
		{"error", errored},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if v := tc.m.View(); strings.TrimSpace(v) == "" {
				t.Error("View() returned empty output")
			}
		})
	}
}

func TestBuildListAndChrome(t *testing.T) {
	m := initialModel(nil)
	m.width, m.height = 100, 30
	m.scanDur = 800 * 1e6 // 0.8s, as time.Duration

	m.buildList([]ProjectInfo{
		{Name: "Alpha", Path: `C:\proj\Alpha.pcwex`, Type: TypePCWEX, Version: "2025.0.0", GitBranch: "main"},
		{Name: "BetaFlat", Path: `C:\proj\BetaFlat`, Type: TypeFlat, Version: "2025.0.0"},
	})

	if m.state != StateList {
		t.Errorf("state after buildList = %v, want StateList", m.state)
	}
	if v := m.View(); !strings.Contains(v, "Alpha") {
		t.Error("list view should contain the project name")
	}
	if h := m.renderHeader(); !strings.Contains(h, "LazyPLCNext") || !strings.Contains(h, "vdev") {
		t.Errorf("header should contain title and version, got %q", h)
	}
	if s := m.renderStatusBar(); !strings.Contains(s, "projects") || !strings.Contains(s, "scanned in") {
		t.Errorf("status bar should contain counts and scan duration, got %q", s)
	}
}
