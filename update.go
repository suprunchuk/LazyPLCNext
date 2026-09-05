package main

import (
	"encoding/json"
	"fmt"
	"io"
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

// doUpdate downloads the release asset from url and applies it. The optional
// progress callback reports downloaded/total bytes as the download advances.
func doUpdate(url string, progress func(downloaded, total int64)) error {
	// Generous timeout: this downloads the full .exe release asset.
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body := io.Reader(resp.Body)
	if progress != nil {
		body = newProgressReader(resp.Body, resp.ContentLength, 256*1024, progress)
	}
	return selfupdate.Apply(body, selfupdate.Options{})
}

// progressReader wraps a reader and emits progress callbacks at fixed byte
// steps plus a final one at EOF, so the UI is not flooded with messages.
type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	lastSent   int64
	step       int64
	onProgress func(downloaded, total int64)
}

func newProgressReader(r io.Reader, total, step int64, fn func(downloaded, total int64)) *progressReader {
	return &progressReader{r: r, total: total, step: step, onProgress: fn}
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.read-p.lastSent >= p.step {
		p.lastSent = p.read
		p.onProgress(p.read, p.total)
	}
	if err == io.EOF && p.read > p.lastSent {
		p.lastSent = p.read
		p.onProgress(p.read, p.total)
	}
	return n, err
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
