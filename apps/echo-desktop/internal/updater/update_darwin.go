//go:build darwin

package updater

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// applyUpdateDarwin downloads the macOS zip, extracts the .app bundle,
// and replaces the running .app in place. macOS does not lock running
// binaries so this works without a helper script.
func applyUpdateDarwin(info *releaseInfo) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}

	// Walk up from Contents/MacOS/VoiceRelay to the .app root
	appPath := exe
	for i := 0; i < 3; i++ {
		appPath = filepath.Dir(appPath)
	}
	if !strings.HasSuffix(appPath, ".app") {
		return fmt.Errorf("not running from .app bundle (resolved to %s)", appPath)
	}

	// Download the zip into memory
	log.Printf("Downloading update for macOS")
	tmpFile, err := os.CreateTemp("", "voicerelay-update-*.zip")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := downloadAsset(info.release.AssetURL, tmpPath); err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}

	zipData, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("reading download: %w", err)
	}

	// Extract zip to a temp directory
	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "voicerelay-update")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	for _, file := range zipReader.File {
		destPath := filepath.Join(tempDir, file.Name)

		// Prevent zip slip
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(tempDir)+string(os.PathSeparator)) {
			continue
		}

		if file.FileInfo().IsDir() {
			os.MkdirAll(destPath, file.Mode())
			continue
		}

		os.MkdirAll(filepath.Dir(destPath), 0755)

		destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return fmt.Errorf("extracting %s: %w", file.Name, err)
		}

		srcFile, err := file.Open()
		if err != nil {
			destFile.Close()
			return fmt.Errorf("reading %s from zip: %w", file.Name, err)
		}

		_, err = io.Copy(destFile, srcFile)
		srcFile.Close()
		destFile.Close()
		if err != nil {
			return fmt.Errorf("writing %s: %w", file.Name, err)
		}
	}

	// Find the extracted .app bundle
	newAppPath := filepath.Join(tempDir, "VoiceRelay.app")
	if _, err := os.Stat(newAppPath); err != nil {
		return fmt.Errorf("extracted zip does not contain VoiceRelay.app")
	}

	// Swap: backup current → move new into place → remove backup
	backupPath := appPath + ".backup"
	os.RemoveAll(backupPath)

	if err := os.Rename(appPath, backupPath); err != nil {
		return fmt.Errorf("backing up current app: %w", err)
	}

	if err := os.Rename(newAppPath, appPath); err != nil {
		// Rollback
		os.Rename(backupPath, appPath)
		return fmt.Errorf("installing new app: %w", err)
	}

	os.RemoveAll(backupPath)
	log.Printf("macOS app bundle updated at %s", appPath)
	return nil
}

func applyUpdateWindows(_ *releaseInfo, _ func()) error {
	panic("applyUpdateWindows called on macOS")
}
