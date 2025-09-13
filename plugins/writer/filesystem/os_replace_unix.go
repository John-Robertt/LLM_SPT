//go:build !windows

package filesystem

import (
    "os"
)

// osReplace performs an atomic rename on POSIX systems.
func osReplace(tmpPath, dest string) error {
    return os.Rename(tmpPath, dest)
}

// syncDir best-effort fsync parent directory to persist metadata.
func syncDir(dir string) error {
    f, err := os.Open(dir)
    if err != nil {
        return err
    }
    defer f.Close()
    if err := f.Sync(); err != nil {
        return err
    }
    return nil
}

