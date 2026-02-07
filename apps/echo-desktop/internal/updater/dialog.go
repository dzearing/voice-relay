package updater

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ncruces/zenity"
)

// CheckForUpdatesInteractive shows a progress dialog while checking for updates
// and prompts the user with the result. The quit function is called if the user
// chooses to restart after installing an update.
func CheckForUpdatesInteractive(quit func()) {
	log.Println("Checking for updates (interactive)...")

	dlg, err := zenity.Progress(
		zenity.Title("Voice Relay Update"),
		zenity.Pulsate(),
	)
	if err != nil {
		log.Printf("Failed to show update dialog: %v", err)
		return
	}

	if err := dlg.Text(fmt.Sprintf("Current version:  %s\nLatest version:   checking...", CurrentVersion)); err != nil {
		dlg.Close()
		return
	}

	info, err := checkLatest()
	if err != nil {
		dlg.Close()
		log.Printf("Update check failed: %v", err)
		zenity.Error(
			fmt.Sprintf("Failed to check for updates:\n%v", err),
			zenity.Title("Voice Relay Update"),
		)
		return
	}

	// Up to date
	if info == nil {
		dlg.Close()
		log.Printf("Already on latest version (%s)", CurrentVersion)
		zenity.Info(
			fmt.Sprintf("Current version:  %s\n\nYou're up to date!", CurrentVersion),
			zenity.Title("Voice Relay Update"),
		)
		return
	}

	// New version available — download and install
	log.Printf("New version available: %s (current: %s)", info.Version, CurrentVersion)

	if err := dlg.Text(fmt.Sprintf("Current version:  %s\nLatest version:   %s\n\nDownloading update...", CurrentVersion, info.Version)); err != nil {
		dlg.Close()
		return
	}

	// On Windows, applyUpdate stages the file and spawns a helper script
	// that swaps the binary after we exit — so we ask first, then apply+quit
	// in one step. On other platforms we apply in-place, then offer restart.
	if runtime.GOOS == "windows" {
		dlg.Close()
		err = zenity.Question(
			fmt.Sprintf("Voice Relay v%s is ready to install.\n\nThe app will restart automatically.", info.Version),
			zenity.Title("Voice Relay Update"),
			zenity.OKLabel("Update"),
			zenity.CancelLabel("Later"),
		)
		if err != nil {
			return // user chose Later
		}

		if err := applyUpdate(info, quit); err != nil {
			log.Printf("Update failed: %v", err)
			zenity.Error(
				fmt.Sprintf("Update failed:\n%v", err),
				zenity.Title("Voice Relay Update"),
			)
		}
		// applyUpdate calls quit; the batch script handles restart.
		return
	}

	// Non-Windows: apply in place, then offer restart.
	if err := applyUpdate(info, nil); err != nil {
		dlg.Close()
		log.Printf("Update failed: %v", err)
		zenity.Error(
			fmt.Sprintf("Update failed:\n%v", err),
			zenity.Title("Voice Relay Update"),
		)
		return
	}

	dlg.Close()
	log.Println("Update installed successfully")

	err = zenity.Question(
		fmt.Sprintf("Updated to Voice Relay v%s!\n\nRestart now to apply the update.", info.Version),
		zenity.Title("Voice Relay Update"),
		zenity.OKLabel("Restart"),
		zenity.CancelLabel("Later"),
	)
	if err == nil {
		restartApp(quit)
	}
}

func restartApp(quit func()) {
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path for restart: %v", err)
		return
	}

	log.Printf("Restarting: %s", execPath)

	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		appPath := execPath
		for i := 0; i < 3; i++ {
			appPath = filepath.Dir(appPath)
		}
		if strings.HasSuffix(appPath, ".app") {
			cmd = exec.Command("open", "-n", appPath)
		} else {
			cmd = exec.Command(execPath)
		}
	} else {
		cmd = exec.Command(execPath)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start new process: %v", err)
		return
	}

	if quit != nil {
		quit()
	}
}
