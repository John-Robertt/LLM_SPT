//go:build windows

package filesystem

import (
    "syscall"
    "unsafe"
)

// Windows MoveFileEx flags
const (
    movefileReplaceExisting = 0x1
    movefileWriteThrough    = 0x8
)

var (
    modkernel32     = syscall.NewLazyDLL("kernel32.dll")
    procMoveFileExW = modkernel32.NewProc("MoveFileExW")
)

// osReplace uses MoveFileExW with REPLACE_EXISTING|WRITE_THROUGH for best-effort atomic replace.
func osReplace(tmpPath, dest string) error {
    fromp, err := syscall.UTF16PtrFromString(tmpPath)
    if err != nil {
        return err
    }
    top, err := syscall.UTF16PtrFromString(dest)
    if err != nil {
        return err
    }
    r1, _, e1 := procMoveFileExW.Call(
        uintptr(unsafe.Pointer(fromp)),
        uintptr(unsafe.Pointer(top)),
        uintptr(movefileReplaceExisting|movefileWriteThrough),
    )
    if r1 == 0 {
        if e1 != nil && e1 != syscall.Errno(0) {
            return e1
        }
        return syscall.EINVAL
    }
    return nil
}

// syncDir is a no-op on Windows; directory fsync is not generally available.
func syncDir(dir string) error { return nil }

