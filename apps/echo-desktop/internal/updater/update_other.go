//go:build !windows && !darwin

package updater

func applyUpdateWindows(_ *releaseInfo, _ func()) error {
	panic("applyUpdateWindows called on non-Windows platform")
}

func applyUpdateDarwin(_ *releaseInfo) error {
	panic("applyUpdateDarwin called on non-macOS platform")
}
