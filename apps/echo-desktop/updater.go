package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	currentVersion = "1.1.0"
	repoOwner      = "dzearing"
	repoName       = "voice-relay"
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func checkForUpdates() {
	log.Println("Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		log.Printf("Update check failed: %v", err)
		return
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == currentVersion {
		log.Printf("Already on latest version (%s)", currentVersion)
		return
	}

	log.Printf("New version available: %s (current: %s)", latestVersion, currentVersion)

	// Find the right asset for this platform
	assetName := getAssetName()
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		log.Printf("No download available for this platform")
		return
	}

	// Download and install
	if err := downloadAndInstall(downloadURL, assetName); err != nil {
		log.Printf("Update failed: %v", err)
		return
	}

	log.Println("Update installed! Please restart the app.")
}

func getLatestRelease() (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func getAssetName() string {
	if runtime.GOOS == "darwin" {
		return "VoiceRelayEcho-macOS-arm64.zip"
	}
	return "VoiceRelayEcho.exe"
}

func downloadAndInstall(url, assetName string) error {
	log.Printf("Downloading %s...", assetName)

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	log.Printf("Downloaded %d bytes", len(data))

	execPath, err := os.Executable()
	if err != nil {
		return err
	}

	if runtime.GOOS == "darwin" {
		// macOS: Extract from zip and replace app bundle
		return installMacOS(data, execPath)
	}

	// Windows: Replace exe directly
	return installWindows(data, execPath)
}

func installMacOS(zipData []byte, execPath string) error {
	// Find the .app bundle path
	// execPath is like /Applications/VoiceRelayEcho.app/Contents/MacOS/VoiceRelayEcho
	appPath := execPath
	for i := 0; i < 3; i++ {
		appPath = filepath.Dir(appPath)
	}

	if !strings.HasSuffix(appPath, ".app") {
		return fmt.Errorf("not running from .app bundle")
	}

	// Extract zip to temp location
	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "voicerelay-update")
	if err != nil {
		return err
	}

	for _, file := range zipReader.File {
		destPath := filepath.Join(tempDir, file.Name)

		if file.FileInfo().IsDir() {
			os.MkdirAll(destPath, file.Mode())
			continue
		}

		os.MkdirAll(filepath.Dir(destPath), 0755)

		destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}

		srcFile, err := file.Open()
		if err != nil {
			destFile.Close()
			return err
		}

		_, err = io.Copy(destFile, srcFile)
		srcFile.Close()
		destFile.Close()
		if err != nil {
			return err
		}
	}

	// Replace the app bundle
	backupPath := appPath + ".backup"
	os.RemoveAll(backupPath)

	if err := os.Rename(appPath, backupPath); err != nil {
		return err
	}

	newAppPath := filepath.Join(tempDir, "VoiceRelayEcho.app")
	if err := os.Rename(newAppPath, appPath); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, appPath)
		return err
	}

	os.RemoveAll(backupPath)
	os.RemoveAll(tempDir)

	return nil
}

func installWindows(exeData []byte, execPath string) error {
	// Rename current exe to .old
	oldPath := execPath + ".old"
	os.Remove(oldPath)

	if err := os.Rename(execPath, oldPath); err != nil {
		return err
	}

	// Write new exe
	if err := os.WriteFile(execPath, exeData, 0755); err != nil {
		// Restore old exe on failure
		os.Rename(oldPath, execPath)
		return err
	}

	// Schedule old exe deletion (will happen on next restart)
	// Windows won't let us delete the running exe
	return nil
}

func getVersion() string {
	return currentVersion
}
