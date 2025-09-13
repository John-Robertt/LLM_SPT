package filesystem

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"llmspt/pkg/contract"
)

// TestWriteAtomic 原子写入
func TestWriteAtomic(t *testing.T) {
    dir := t.TempDir()
    a := true
    w, err := New(&Options{OutputDir: dir, Atomic: &a})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	err = w.Write(context.Background(), "out.txt", bytes.NewBufferString("data"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(b) != "data" {
		t.Fatalf("unexpected file %v %q", err, string(b))
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("tmp file not cleaned: %s", e.Name())
		}
	}
}

// 当目标已存在时，Atomic 写应替换为新内容（跨平台）。
func TestWriteAtomicReplaceExisting(t *testing.T) {
    dir := t.TempDir()
    a := true
    w, err := New(&Options{OutputDir: dir, Atomic: &a})
    if err != nil {
        t.Fatalf("new: %v", err)
    }
    if err := w.Write(context.Background(), "out.txt", bytes.NewBufferString("v1")); err != nil {
        t.Fatalf("write v1: %v", err)
    }
    if err := w.Write(context.Background(), "out.txt", bytes.NewBufferString("v2")); err != nil {
        t.Fatalf("write v2: %v", err)
    }
    b, err := os.ReadFile(filepath.Join(dir, "out.txt"))
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    if string(b) != "v2" {
        t.Fatalf("expect replaced content v2, got %q", string(b))
    }
    // 不应残留临时文件
    entries, _ := os.ReadDir(dir)
    for _, e := range entries {
        if strings.HasPrefix(e.Name(), ".tmp-") {
            t.Fatalf("tmp file not cleaned: %s", e.Name())
        }
    }
}

// TestWritePathInvalid 路径越界
func TestWritePathInvalid(t *testing.T) {
    dir := t.TempDir()
    flat := false
    w, _ := New(&Options{OutputDir: dir, Flat: &flat})
    err := w.Write(context.Background(), "../bad", bytes.NewBufferString("x"))
    if err == nil || err != contract.ErrPathInvalid {
        t.Fatalf("expect path invalid, got %v", err)
    }
}

// TestWriteNonAtomic 非原子写入
func TestWriteNonAtomic(t *testing.T) {
	dir := t.TempDir()
	flat := false
	w, _ := New(&Options{OutputDir: dir, Flat: &flat})
	err := w.Write(context.Background(), "sub/out.txt", bytes.NewBufferString("v"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "out.txt")); err != nil {
		t.Fatalf("file not created")
	}
}

// TestWriteCtxCancel 上下文取消
func TestWriteCtxCancel(t *testing.T) {
	dir := t.TempDir()
	w, _ := New(&Options{OutputDir: dir})
	r := strings.NewReader("data")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Write(ctx, "a.txt", r); err == nil {
		t.Fatalf("expect ctx error")
	}
}

// TestNewInvalid 参数缺失
func TestNewInvalid(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatalf("expect error for nil opts")
	}
	if _, err := New(&Options{}); err == nil {
		t.Fatalf("expect error for empty output dir")
	}
}


type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }

// TestWriteAtomicCopyError 原子写入时拷贝失败
func TestWriteAtomicCopyError(t *testing.T) {
    dir := t.TempDir()
    a := true
    w, _ := New(&Options{OutputDir: dir, Atomic: &a})
	err := w.Write(context.Background(), "a.txt", errReader{})
	if err == nil {
		t.Fatalf("expect copy error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("temp files left %v", entries)
	}
}

// TestReaderWithCtxCancel reader 在读取前取消
func TestReaderWithCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := readerWithCtx(ctx, strings.NewReader("data"))
	cancel()
	buf := make([]byte, 1)
	if _, err := r.Read(buf); err == nil {
		t.Fatalf("expect ctx error")
	}
}
