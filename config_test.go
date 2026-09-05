package main

import (
	"testing"
)

func TestParseConfig(t *testing.T) {
	cases := []struct {
		name     string
		json     string
		wantDir  string
		wantNerd bool
	}{
		{
			name:     "current format",
			json:     `{"work_dir": "D:\\My_PLC_Projects", "use_nerd_fonts": true}`,
			wantDir:  `D:\My_PLC_Projects`,
			wantNerd: true,
		},
		{
			name:    "legacy work_dirs migrates to first entry",
			json:    `{"work_dirs": ["D:\\A", "D:\\B"]}`,
			wantDir: `D:\A`,
		},
		{
			name:    "legacy with empty list",
			json:    `{"work_dirs": []}`,
			wantDir: "",
		},
		{
			name:    "empty config",
			json:    `{}`,
			wantDir: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig([]byte(tc.json))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.WorkDir != tc.wantDir {
				t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, tc.wantDir)
			}
			if cfg.UseNerdFonts != tc.wantNerd {
				t.Errorf("UseNerdFonts = %v, want %v", cfg.UseNerdFonts, tc.wantNerd)
			}
		})
	}

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := parseConfig([]byte("not json")); err == nil {
			t.Error("expected error for malformed json")
		}
	})
}
