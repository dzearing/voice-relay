package stt

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
	"strings"
)

const (
	modelBaseURL   = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main"
	whisperRepoAPI = "https://api.github.com/repos/ggml-org/whisper.cpp/releases/latest"
)

func modelFileName(name string) string {
	return fmt.Sprintf("ggml-%s.bin", name)
}

// EnsureModel checks if the whisper model exists and downloads it if not.
func EnsureModel(modelsDir, name string) (string, error) {
	filename := modelFileName(name)
	modelPath := filepath.Join(modelsDir, filename)

	if _, err := os.Stat(modelPath); err == nil {
		log.Printf("Whisper model found: %s", modelPath)
		return modelPath, nil
	}

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create models directory: %w", err)
	}

	url := fmt.Sprintf("%s/%s", modelBaseURL, filename)
	log.Printf("Downloading whisper model: %s", url)

	if err := downloadFile(modelPath, url); err != nil {
		return "", fmt.Errorf("failed to download model: %w", err)
	}

	log.Printf("Whisper model downloaded: %s", modelPath)
	return modelPath, nil
}

// EnsureServer checks if whisper-server binary exists and downloads it if not.
func EnsureServer(binDir string) (string, error) {
	serverPath := WhisperServerPath(filepath.Dir(binDir))
	if filepath.Dir(binDir) == binDir {
		// binDir is the parent; WhisperServerPath adds "bin/"
		serverPath = WhisperServerPath(binDir)
	}
	// Simplify: just look for the binary in binDir/bin/
	serverPath = filepath.Join(binDir, ServerBinaryName())

	if _, err := os.Stat(serverPath); err == nil {
		log.Printf("whisper-server found: %s", serverPath)
		return serverPath, nil
	}

	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Get download URL from GitHub releases
	downloadURL, err := getWhisperServerURL()
	if err != nil {
		return "", fmt.Errorf("failed to get whisper-server download URL: %w", err)
	}

	log.Printf("Downloading whisper-server: %s", downloadURL)

	zipData, err := downloadBytes(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download whisper-server: %w", err)
	}

	// Extract the server binary from the zip
	if err := extractServerFromZip(zipData, binDir); err != nil {
		return "", fmt.Errorf("failed to extract whisper-server: %w", err)
	}

	if _, err := os.Stat(serverPath); err != nil {
		return "", fmt.Errorf("whisper-server binary not found after extraction at %s", serverPath)
	}

	log.Printf("whisper-server installed: %s", serverPath)
	return serverPath, nil
}

func getWhisperServerURL() (string, error) {
	resp, err := http.Get(whisperRepoAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	targetAsset := WhisperServerAssetName()
	for _, asset := range release.Assets {
		if asset.Name == targetAsset {
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("asset %s not found in release", targetAsset)
}

func extractServerFromZip(zipData []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}

	serverName := ServerBinaryName()

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		// Extract all exe/binary files — we need whisper-server plus any DLLs
		if strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".dll") ||
			name == serverName || strings.HasPrefix(name, "whisper") ||
			strings.HasPrefix(name, "ggml") {
			destPath := filepath.Join(destDir, name)

			rc, err := f.Open()
			if err != nil {
				return err
			}

			out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				rc.Close()
				return err
			}

			_, err = io.Copy(out, rc)
			rc.Close()
			out.Close()
			if err != nil {
				return err
			}

			log.Printf("Extracted: %s", name)
		}
	}

	return nil
}

func downloadFile(dest, url string) error {
	data, err := downloadBytes(url)
	if err != nil {
		return err
	}

	tmpPath := dest + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, dest)
}

func downloadBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Printf("Downloaded %d bytes", len(data))
	return data, nil
}
