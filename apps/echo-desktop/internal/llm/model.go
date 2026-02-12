package llm

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	llamaRepoAPI = "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest"
)

// EnsureModel checks if the LLM model (GGUF) exists and downloads it if missing.
func EnsureModel(modelsDir, name string) (string, error) {
	filename := getModelFilename(name)
	modelPath := filepath.Join(modelsDir, filename)

	if _, err := os.Stat(modelPath); err == nil {
		log.Printf("LLM model found: %s", modelPath)
		return modelPath, nil
	}

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create models directory: %w", err)
	}

	url := getModelURL(name)
	if url == "" {
		return "", fmt.Errorf("unknown model: %s", name)
	}

	log.Printf("Downloading LLM model: %s", url)

	if err := downloadFile(modelPath, url); err != nil {
		return "", fmt.Errorf("failed to download model: %w", err)
	}

	log.Printf("LLM model downloaded: %s", modelPath)
	return modelPath, nil
}

// EnsureServer checks if llama-server binary exists and downloads it if not.
// Uses a "llama" subdirectory to avoid DLL conflicts with whisper-server.
func EnsureServer(binDir string) (string, error) {
	llamaDir := filepath.Join(binDir, "llama")
	serverPath := filepath.Join(llamaDir, ServerBinaryName())

	if _, err := os.Stat(serverPath); err == nil {
		log.Printf("llama-server found: %s", serverPath)
		return serverPath, nil
	}

	if err := os.MkdirAll(llamaDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create llama bin directory: %w", err)
	}

	downloadURL, err := getLlamaServerURL()
	if err != nil {
		return "", fmt.Errorf("failed to get llama-server download URL: %w", err)
	}

	log.Printf("Downloading llama-server: %s", downloadURL)

	zipData, err := downloadBytes(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download llama-server: %w", err)
	}

	if err := extractServerFromZip(zipData, llamaDir); err != nil {
		return "", fmt.Errorf("failed to extract llama-server: %w", err)
	}

	if _, err := os.Stat(serverPath); err != nil {
		return "", fmt.Errorf("llama-server binary not found after extraction at %s", serverPath)
	}

	log.Printf("llama-server installed: %s", serverPath)
	return serverPath, nil
}

func getModelURL(name string) string {
	urls := map[string]string{
		"qwen3-4b": "https://huggingface.co/bartowski/Qwen_Qwen3-4B-GGUF/resolve/main/Qwen_Qwen3-4B-Q4_K_M.gguf",
	}
	return urls[name]
}

func getModelFilename(name string) string {
	filenames := map[string]string{
		"qwen3-4b": "Qwen_Qwen3-4B-Q4_K_M.gguf",
	}
	if fn, ok := filenames[name]; ok {
		return fn
	}
	return name + ".gguf"
}

// ServerBinaryName returns the platform-specific llama-server binary name.
func ServerBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

// HasNvidiaGPU returns true if an NVIDIA GPU is detected on the system.
func HasNvidiaGPU() bool {
	cmd := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// llamaServerAssetSuffix returns the suffix to match in llama.cpp release asset names.
// Assets are named like "llama-b7951-bin-win-cpu-x64.zip" with a version prefix.
// On Windows with an NVIDIA GPU, prefers the CUDA build for GPU offloading.
func llamaServerAssetSuffix() string {
	switch runtime.GOOS {
	case "windows":
		if HasNvidiaGPU() {
			log.Printf("NVIDIA GPU detected, using CUDA build of llama-server")
			return "bin-win-cuda-cu12.2-x64.zip"
		}
		return "bin-win-cpu-x64.zip"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "bin-macos-arm64.tar.gz"
		}
		return "bin-macos-x64.tar.gz"
	default:
		return "bin-ubuntu-x64.tar.gz"
	}
}

func getLlamaServerURL() (string, error) {
	resp, err := http.Get(llamaRepoAPI)
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

	suffix := llamaServerAssetSuffix()
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, suffix) {
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("asset with suffix %s not found in release", suffix)
}

func extractServerFromZip(zipData []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}

	serverName := ServerBinaryName()

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		// Extract server binary and any shared libraries
		if strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".dll") ||
			name == serverName || strings.HasPrefix(name, "llama") ||
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
