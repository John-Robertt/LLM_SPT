package filesystem

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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


