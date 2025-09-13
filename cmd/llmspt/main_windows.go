//go:build windows

package main

import "time"

// windowsFileCleanupDelay adds a small delay on Windows to allow file handles to be fully released
func windowsFileCleanupDelay() {
	time.Sleep(500 * time.Millisecond) // Increased delay for Windows
}