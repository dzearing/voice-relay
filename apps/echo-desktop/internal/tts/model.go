package tts

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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
	piperRepoAPI = "https://api.github.com/repos/rhasspy/piper/releases/latest"

	// Voice model URLs (HuggingFace)
	defaultVoiceBaseURL = "https://huggingface.co/rhasspy/piper-voices/resolve/v1.0.0"
	defaultVoicePath    = "en/en_US/lessac/medium"
	defaultVoiceName    = "en_US-lessac-high"
)

// BinaryName returns the platform-specific piper binary name.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "piper.exe"
	}
	return "piper"
}

// piperAssetName returns the piper release asset name for this platform.
func piperAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "piper_windows_amd64.zip"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "piper_macos_aarch64.tar.gz"
		}
		return "piper_macos_x64.tar.gz"
	default:
		return "piper_linux_x86_64.tar.gz"
	}
}

// EnsureServer checks if the piper binary exists and downloads it if not.
// Returns the path to the piper binary.
func EnsureServer(binDir string) (string, error) {
	piperDir := filepath.Join(binDir, "piper")
	serverPath := filepath.Join(piperDir, BinaryName())

	if _, err := os.Stat(serverPath); err == nil {
		log.Printf("Piper binary found: %s", serverPath)
		return serverPath, nil
	}

	if err := os.MkdirAll(piperDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create piper bin directory: %w", err)
	}

	downloadURL, err := getPiperDownloadURL()
	if err != nil {
		return "", fmt.Errorf("failed to get piper download URL: %w", err)
	}

	log.Printf("Downloading piper: %s", downloadURL)

	archiveData, err := downloadBytes(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download piper: %w", err)
	}

	assetName := piperAssetName()
	if strings.HasSuffix(assetName, ".zip") {
		if err := extractZip(archiveData, piperDir); err != nil {
			return "", fmt.Errorf("failed to extract piper zip: %w", err)
		}
	} else {
		if err := extractTarGz(archiveData, piperDir); err != nil {
			return "", fmt.Errorf("failed to extract piper tar.gz: %w", err)
		}
	}

	if _, err := os.Stat(serverPath); err != nil {
		return "", fmt.Errorf("piper binary not found after extraction at %s", serverPath)
	}

	log.Printf("Piper installed: %s", serverPath)
	return serverPath, nil
}

// EnsureVoice checks if a voice model exists and downloads it if not.
// Returns the path to the .onnx model file.
func EnsureVoice(modelsDir, voiceName string) (string, error) {
	if voiceName == "" || voiceName == "default" {
		voiceName = defaultVoiceName
	}

	modelPath := filepath.Join(modelsDir, voiceName+".onnx")
	jsonPath := filepath.Join(modelsDir, voiceName+".onnx.json")

	// Check if both files exist
	if _, err := os.Stat(modelPath); err == nil {
		if _, err := os.Stat(jsonPath); err == nil {
			log.Printf("Piper voice found: %s", modelPath)
			return modelPath, nil
		}
	}

	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create models directory: %w", err)
	}

	// Build HuggingFace URLs
	voicePath := voiceToPath(voiceName)
	onnxURL := fmt.Sprintf("%s/%s/%s.onnx", defaultVoiceBaseURL, voicePath, voiceName)
	jsonURL := fmt.Sprintf("%s/%s/%s.onnx.json", defaultVoiceBaseURL, voicePath, voiceName)

	// Download .onnx model
	log.Printf("Downloading Piper voice model: %s", onnxURL)
	if err := downloadFile(modelPath, onnxURL); err != nil {
		return "", fmt.Errorf("failed to download voice model: %w", err)
	}

	// Download .onnx.json config
	log.Printf("Downloading Piper voice config: %s", jsonURL)
	if err := downloadFile(jsonPath, jsonURL); err != nil {
		return "", fmt.Errorf("failed to download voice config: %w", err)
	}

	log.Printf("Piper voice downloaded: %s", modelPath)
	return modelPath, nil
}

// voiceToPath converts a voice name like "en_US-lessac-high" to
// the HuggingFace path "en/en_US/lessac/medium".
func voiceToPath(voiceName string) string {
	// Format: {lang}_{REGION}-{name}-{quality}
	// e.g. en_US-lessac-high -> en/en_US/lessac/medium
	parts := strings.SplitN(voiceName, "-", 3)
	if len(parts) != 3 {
		return defaultVoicePath
	}

	locale := parts[0]  // en_US
	name := parts[1]    // lessac
	quality := parts[2] // medium

	lang := strings.SplitN(locale, "_", 2)[0] // en

	return fmt.Sprintf("%s/%s/%s/%s", lang, locale, name, quality)
}

func getPiperDownloadURL() (string, error) {
	resp, err := http.Get(piperRepoAPI)
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

	targetAsset := piperAssetName()
	for _, asset := range release.Assets {
		if asset.Name == targetAsset {
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("asset %s not found in release", targetAsset)
}

// extractZip extracts all files from a zip archive into destDir.
// Files nested inside a top-level directory are flattened into destDir.
func extractZip(zipData []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Strip the top-level "piper/" directory prefix if present
		name := f.Name
		if idx := strings.Index(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if name == "" {
			continue
		}

		destPath := filepath.Join(destDir, name)

		// Create subdirectories (e.g. espeak-ng-data/)
		if dir := filepath.Dir(destPath); dir != destDir {
			os.MkdirAll(dir, 0755)
		}

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

	return nil
}

// extractTarGz extracts all files from a .tar.gz archive into destDir.
// Files nested inside a top-level directory are flattened into destDir.
func extractTarGz(data []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if header.Typeflag == tar.TypeDir {
			continue
		}

		// Strip the top-level "piper/" directory prefix if present
		name := header.Name
		if idx := strings.Index(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if name == "" {
			continue
		}

		destPath := filepath.Join(destDir, name)

		// Create subdirectories (e.g. espeak-ng-data/)
		if dir := filepath.Dir(destPath); dir != destDir {
			os.MkdirAll(dir, 0755)
		}

		out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}

		_, err = io.Copy(out, tr)
		out.Close()
		if err != nil {
			return err
		}

		log.Printf("Extracted: %s", name)
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
