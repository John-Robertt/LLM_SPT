//go:build !windows

package filesystem

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"llmspt/pkg/contract"
)

// TestWalkDirNonRegular 非常规文件被忽略 (Unix only - uses mkfifo)
func TestWalkDirNonRegular(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	r := New(nil)
	var visited []string
	err := r.Iterate(context.Background(), []string{root}, func(id contract.FileID, rc io.ReadCloser) error {
		visited = append(visited, string(id))
		rc.Close()
		return nil
	})
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if len(visited) != 0 {
		t.Fatalf("non-regular should skip, visited %#v", visited)
	}
}

// TestIterateSymlink 测试符号链接 (Unix only)
func TestIterateSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	os.WriteFile(target, []byte("ok"), 0o644)
	link := filepath.Join(dir, "l.txt")
	os.Symlink(target, link)
	r := New(nil)
	var visited []string
	r.Iterate(context.Background(), []string{link}, func(id contract.FileID, rc io.ReadCloser) error {
		visited = append(visited, string(id))
		rc.Close()
		return nil
	})
	if len(visited) != 1 || !strings.Contains(visited[0], "l.txt") {
		t.Fatalf("symlink not visited: %#v", visited)
	}
}

// TestIterateSymlinkDir 符号链接指向目录时忽略 (Unix only)
func TestIterateSymlinkDir(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	os.Mkdir(realDir, 0o755)
	os.WriteFile(filepath.Join(realDir, "a.txt"), []byte("x"), 0o644)
	link := filepath.Join(root, "ln")
	os.Symlink(realDir, link)
	r := New(nil)
	var visited []string
	err := r.Iterate(context.Background(), []string{link}, func(id contract.FileID, rc io.ReadCloser) error {
		visited = append(visited, string(id))
		rc.Close()
		return nil
	})
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if len(visited) != 0 {
		t.Fatalf("dir symlink visited: %#v", visited)
	}
}

// TestWalkDirSymlinkDir 遍历目录时忽略指向目录的符号链接 (Unix only)
func TestWalkDirSymlinkDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "ok.txt"), []byte("o"), 0o644)
	// 创建指向目录的符号链接
	os.Symlink(sub, filepath.Join(root, "sub_link"))
	r := New(nil)
	var files []string
	err := r.Iterate(context.Background(), []string{root}, func(id contract.FileID, rc io.ReadCloser) error {
		files = append(files, filepath.Base(string(id)))
		rc.Close()
		return nil
	})
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if len(files) != 1 || files[0] != "ok.txt" {
		t.Fatalf("unexpected files %#v", files)
	}
}

// TestIterateSymlinkDangling 符号链接失效返回错误 (Unix only)
func TestIterateSymlinkDangling(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling")
	os.Symlink(filepath.Join(dir, "no"), link)
	r := New(nil)
	err := r.Iterate(context.Background(), []string{link}, func(contract.FileID, io.ReadCloser) error { return nil })
	if err == nil {
		t.Fatalf("expect error for dangling symlink")
	}
}