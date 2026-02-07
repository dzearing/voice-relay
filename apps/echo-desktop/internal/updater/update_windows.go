//go:build windows

package updater

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// applyUpdateWindows downloads the new exe to a staging file, writes a helper
// PowerShell script that waits for our process to exit, swaps the files, and
// relaunches the app. The caller's quit function is invoked so the current
// process exits and the script can proceed.
func applyUpdateWindows(info *releaseInfo, quit func()) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}

	dir := filepath.Dir(exe)
	base := filepath.Base(exe)
	staged := filepath.Join(dir, base+".new")
	script := filepath.Join(dir, "update.ps1")

	log.Printf("Downloading update to %s", staged)
	if err := downloadAsset(info.release.AssetURL, staged); err != nil {
		os.Remove(staged)
		return fmt.Errorf("downloading update: %w", err)
	}

	pid := os.Getpid()

	// PowerShell script that:
	// 1. Waits for our process to fully exit
	// 2. Retries the move up to 10 times (file lock may linger briefly)
	// 3. Starts the new exe
	// 4. Cleans up staged file and script
	ps := fmt.Sprintf(
		"try { Wait-Process -Id %d -Timeout 30 -ErrorAction SilentlyContinue } catch {}\r\n"+
			"Start-Sleep -Seconds 1\r\n"+
			"$ok = $false\r\n"+
			"for ($i = 0; $i -lt 10; $i++) {\r\n"+
			"  try {\r\n"+
			"    Move-Item -Path '%s' -Destination '%s' -Force\r\n"+
			"    $ok = $true\r\n"+
			"    break\r\n"+
			"  } catch {\r\n"+
			"    Start-Sleep -Seconds 1\r\n"+
			"  }\r\n"+
			"}\r\n"+
			"if ($ok) {\r\n"+
			"  Start-Process -FilePath '%s'\r\n"+
			"}\r\n"+
			"Remove-Item -Path '%s' -Force -ErrorAction SilentlyContinue\r\n",
		pid,
		staged, exe,
		exe,
		script,
	)
	if err := os.WriteFile(script, []byte(ps), 0755); err != nil {
		os.Remove(staged)
		return fmt.Errorf("writing update script: %w", err)
	}

	log.Printf("Launching update script: %s", script)
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-File", script,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		os.Remove(staged)
		os.Remove(script)
		return fmt.Errorf("launching update script: %w", err)
	}

	cmd.Process.Release()

	log.Println("Update staged — exiting for script to swap binary")

	// Start graceful shutdown in the background (closes child processes,
	// systray, etc.) but don't wait for it — hard-exit after a deadline
	// so the helper script can swap the binary.
	if quit != nil {
		go quit()
	}
	time.Sleep(2 * time.Second)
	os.Exit(0)

	return nil // unreachable
}

func applyUpdateDarwin(_ *releaseInfo) error {
	panic("applyUpdateDarwin called on Windows")
}
