package updater

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"

	"github.com/creativeprojects/go-selfupdate"
)

// CurrentVersion is the default version, overridden at build time via:
//
//	-ldflags "-X github.com/voice-relay/echo-desktop/internal/updater.CurrentVersion=1.2.3"
var CurrentVersion = "0.0.0-dev"

const (
	repoOwner = "dzearing"
	repoName  = "voice-relay"
)

// newUpdater returns an Updater configured with a filter for the current
// platform's asset name. Filters bypass the library's default OS/arch suffix
// matching, which doesn't recognise our custom asset names.
func newUpdater() (*selfupdate.Updater, error) {
	// The library lowercases asset names before matching, so use (?i).
	filter := `(?i)^voicerelay\.exe$`
	if runtime.GOOS == "darwin" {
		filter = `(?i)^voicerelay-macos-arm64\.zip$`
	}
	return selfupdate.NewUpdater(selfupdate.Config{
		Filters: []string{filter},
	})
}

// releaseInfo holds the detected latest release metadata.
type releaseInfo struct {
	Version string
	release *selfupdate.Release
}

// checkLatest queries GitHub for the latest release.
// Returns nil (no error) when already up to date.
func checkLatest() (*releaseInfo, error) {
	up, err := newUpdater()
	if err != nil {
		return nil, fmt.Errorf("creating updater: %w", err)
	}

	latest, found, err := up.DetectLatest(context.Background(), selfupdate.ParseSlug(repoOwner+"/"+repoName))
	if err != nil {
		return nil, fmt.Errorf("detecting latest release: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("no release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if latest.LessOrEqual(CurrentVersion) {
		return nil, nil
	}

	return &releaseInfo{
		Version: latest.Version(),
		release: latest,
	}, nil
}

// applyUpdate downloads the release asset and replaces the running binary.
//
// On Windows, we download to a staging file, spawn a helper script, and exit;
// the script waits for us to die, swaps the file, and relaunches.
//
// On macOS, we download the zip, extract the .app bundle, and swap it in
// place (macOS doesn't lock running binaries).
func applyUpdate(info *releaseInfo, quit func()) error {
	switch runtime.GOOS {
	case "windows":
		return applyUpdateWindows(info, quit)
	case "darwin":
		return applyUpdateDarwin(info)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// downloadAsset downloads the release asset URL to the given path.
func downloadAsset(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// CheckForUpdates checks GitHub for a newer release and stages it silently.
func CheckForUpdates() {
	log.Println("Checking for updates...")

	info, err := checkLatest()
	if err != nil {
		log.Printf("Update check failed: %v", err)
		return
	}
	if info == nil {
		log.Printf("Already on latest version (%s)", CurrentVersion)
		return
	}

	log.Printf("New version available: %s (current: %s)", info.Version, CurrentVersion)
	// Don't auto-apply on Windows; the interactive dialog handles the
	// download + restart flow. Just log the availability.
	if runtime.GOOS == "windows" {
		return
	}

	if err := applyUpdate(info, nil); err != nil {
		log.Printf("Update failed: %v", err)
		return
	}

	log.Println("Update installed! Please restart the app.")
}
