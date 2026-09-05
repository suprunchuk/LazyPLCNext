package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

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
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
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
	// Generous timeout: this downloads the full .exe release asset.
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return selfupdate.Apply(resp.Body, selfupdate.Options{})
}

func cleanupOldVersion() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	oldExe := exe + ".old"
	if _, err := os.Stat(oldExe); err == nil {
		_ = os.Remove(oldExe)
	}
}

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
