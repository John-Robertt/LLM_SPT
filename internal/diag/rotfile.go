package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RotatingFile 将日志行写入指定目录，并按文件大小轮转。
// - 当前文件固定名：llmspt-current.txt
// - 轮转：当 size+len(line) 超过 maxBytes 时，将当前文件重命名为 llmspt-YYYYMMDD-HHMMSS.txt，重新创建 llmspt-current.txt。
type RotatingFile struct {
	dir      string
	maxBytes int64
	mu       sync.Mutex
	f        *os.File
	curSize  int64
}

func NewRotatingFile(dir string, maxBytes int64) *RotatingFile {
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024 // 10 MiB 默认
	}
	return &RotatingFile{dir: dir, maxBytes: maxBytes}
}

func (w *RotatingFile) WriteLine(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	lineLen := int64(len(b) + 1) // 包含换行
	if err := w.ensureOpen(); err != nil {
		return err
	}
	if w.curSize+lineLen > w.maxBytes {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	n, err := w.f.Write(append(b, '\n'))
	if err != nil {
		return err
	}
	w.curSize += int64(n)
	return nil
}

func (w *RotatingFile) ensureOpen() error {
	if w.f != nil {
		return nil
	}
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	name := filepath.Join(w.dir, "llmspt-current.txt")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	// 初始化当前大小
	if st, err := f.Stat(); err == nil {
		w.curSize = st.Size()
	} else {
		w.curSize = 0
	}
	return nil
}

func (w *RotatingFile) rotate() error {
	if w.f == nil {
		return w.ensureOpen()
	}
	oldPath := w.f.Name()
	_ = w.f.Close()
	w.f = nil
    // 目标名称：带高精度时间戳，避免同秒冲突覆盖
    ts := time.Now().UTC().Format("20060102-150405.000000000")
    rotated := filepath.Join(filepath.Dir(oldPath), fmt.Sprintf("llmspt-%s.txt", ts))
    // 重命名：将 current.txt 移动为带时间戳文件
    if err := os.Rename(oldPath, rotated); err != nil {
        return fmt.Errorf("rename rotated file: %w", err)
    }
	// 打开新 current
	return w.ensureOpen()
}

// Close 关闭当前打开的文件句柄
func (w *RotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f != nil {
		err := w.f.Close()
		w.f = nil
		return err
	}
	return nil
}
