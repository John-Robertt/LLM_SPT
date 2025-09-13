//go:build !windows

package main

// windowsFileCleanupDelay is a no-op on Unix systems
func windowsFileCleanupDelay() {
	// No delay needed on Unix systems
}