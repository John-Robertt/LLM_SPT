package filesystem

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"llmspt/pkg/contract"
)

// TestIterateSingleFile 读取单文件
func TestIterateSingleFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "a.txt")
	os.WriteFile(fp, []byte("hello"), 0o644)
	r := New(nil)
	var got []byte
	err := r.Iterate(context.Background(), []string{fp}, func(id contract.FileID, rc io.ReadCloser) error {
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		got = append(got, b...)
		if id != contract.NormalizeFileID(fp) {
			t.Fatalf("file id mismatch %s", id)
		}
		return nil
	})
	if err != nil || string(got) != "hello" {
		t.Fatalf("iterate: %v %q", err, string(got))
	}
}

// TestExcludeDir 跳过目录
func TestExcludeDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("k"), 0o644)
	skipDir := filepath.Join(dir, "skip")
	os.Mkdir(skipDir, 0o755)
	os.WriteFile(filepath.Join(skipDir, "bad.txt"), []byte("b"), 0o644)

	r := New(&Options{ExcludeDirNames: []string{"skip"}})
	var files []string
	err := r.Iterate(context.Background(), []string{dir}, func(id contract.FileID, rc io.ReadCloser) error {
		files = append(files, string(id))
		rc.Close()
		return nil
	})
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if len(files) != 1 || !strings.Contains(files[0], "keep.txt") {
		t.Fatalf("exclude failed: %#v", files)
	}
}

// TestIterateSymlink 测试符号链接
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

// TestIterateDashMix 混用 '-' 返回错误
func TestIterateDashMix(t *testing.T) {
	r := New(nil)
	err := r.Iterate(context.Background(), []string{"-", "a"}, func(contract.FileID, io.ReadCloser) error { return nil })
	if err == nil {
		t.Fatalf("expect error for dash mix")
	}
}

// TestIterateStdinNil roots 为空时读取 STDIN
func TestIterateStdinNil(t *testing.T) {
	r := New(nil)
	old := os.Stdin
	pr, pw, _ := os.Pipe()
	os.Stdin = pr
	defer func() { os.Stdin = old }()
	go func() {
		pw.Write([]byte("hi"))
		pw.Close()
	}()
	var data []byte
	err := r.Iterate(context.Background(), nil, func(id contract.FileID, rc io.ReadCloser) error {
		defer rc.Close()
		if id != "stdin" {
			t.Fatalf("id=%s", id)
		}
		b, _ := io.ReadAll(rc)
		data = b
		return nil
	})
	if err != nil || string(data) != "hi" {
		t.Fatalf("stdin nil: %v %q", err, string(data))
	}
}

// TestIterateStdinDash roots 包含 '-' 时读取 STDIN
func TestIterateStdinDash(t *testing.T) {
	r := New(nil)
	old := os.Stdin
	pr, pw, _ := os.Pipe()
	os.Stdin = pr
	defer func() { os.Stdin = old }()
	go func() {
		pw.Write([]byte("ok"))
		pw.Close()
	}()
	var data []byte
	err := r.Iterate(context.Background(), []string{"-"}, func(id contract.FileID, rc io.ReadCloser) error {
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		data = b
		return nil
	})
	if err != nil || string(data) != "ok" {
		t.Fatalf("stdin dash: %v %q", err, string(data))
	}
}

// TestIterateSymlinkDir 符号链接指向目录时忽略
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

// TestWalkDirSymlinkDir 遍历目录时忽略指向目录的符号链接
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

// TestIterateCtxCancel 上下文取消
func TestIterateCtxCancel(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "a.txt")
	os.WriteFile(fp, []byte("x"), 0o644)
	r := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.Iterate(ctx, []string{fp}, func(contract.FileID, io.ReadCloser) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expect ctx cancel, got %v", err)
	}
}

// TestNewBufferedCloserDefault bufSize<=0 时使用默认
func TestNewBufferedCloserDefault(t *testing.T) {
	r := io.NopCloser(strings.NewReader(""))
	bc := newBufferedCloser(r, 0)
	if bc.Reader == nil {
		t.Fatalf("nil reader")
	}
	bc.Close()
}

// TestIterateSymlinkDangling 符号链接失效返回错误
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

// TestWalkDirNonRegular 非常规文件被忽略
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
