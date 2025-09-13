package filesystem

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"llmspt/pkg/contract"
)

// Options: 最小必要选项。
type Options struct {
    // OutputDir: 输出根目录（必需）。
    OutputDir string `json:"output_dir"`
    // Atomic: 是否使用原子替换（同目录临时文件 + rename）。
    // 默认值：true。未提供该字段时采用原子写；显式 false 可关闭。
    Atomic *bool `json:"atomic,omitempty"`
	// Flat: 是否扁平化输出（仅保留文件名，不保留目录层级）。
	// 默认 true；当为 nil 时采用默认 true；显式 false 覆盖。
	Flat *bool `json:"flat,omitempty"`
	// PermFile/PermDir: 可选权限；为 0 表示使用实现/平台默认。
	PermFile os.FileMode `json:"perm_file,omitempty"`
	PermDir  os.FileMode `json:"perm_dir,omitempty"`
	// BufSize: 写缓冲区大小；<=0 使用实现默认。
	BufSize int `json:"buf_size,omitempty"`
}

type FS struct {
    root    string
    atomic  bool
	flat    bool
	permF   os.FileMode
	permD   os.FileMode
	bufSize int
}

// New 创建文件系统 Writer 实现。
func New(opts *Options) (*FS, error) {
    if opts == nil || strings.TrimSpace(opts.OutputDir) == "" {
        return nil, os.ErrInvalid
    }
    bsz := opts.BufSize
    if bsz <= 0 {
        bsz = 64 * 1024
    }
    pf := opts.PermFile
    if pf == 0 {
        pf = 0o644
    }
    pd := opts.PermDir
    if pd == 0 {
        pd = 0o755
    }
    flat := true
    if opts.Flat != nil {
        flat = *opts.Flat
    }
    atomic := true
    if opts.Atomic != nil {
        atomic = *opts.Atomic
    }
    return &FS{root: opts.OutputDir, atomic: atomic, flat: flat, permF: pf, permD: pd, bufSize: bsz}, nil
}

var _ contract.Writer = (*FS)(nil)

// Write 将 r 的全部字节写入到基于 id 映射的目标路径。
func (w *FS) Write(ctx context.Context, id contract.ArtifactID, r io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	dest, err := w.mapPath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), w.permD); err != nil {
		return err
	}

	if w.atomic {
		return w.writeAtomic(ctx, dest, r)
	}
	return w.writeOverwrite(ctx, dest, r)
}

// mapPath: Clean + Join + 越界校验。
func (w *FS) mapPath(id contract.ArtifactID) (string, error) {
    rel := filepath.Clean(string(id))
    // Flat 优先：若扁平化，则仅保留文件名并在此后校验名称合法
    if w.flat {
        rel = filepath.Base(rel)
        if rel == "." || rel == ".." || rel == "" {
            return "", contract.ErrPathInvalid
        }
        return filepath.Join(w.root, rel), nil
    }
    // 非扁平：禁止绝对路径、父级逃逸、Windows 卷名
    if rel == "." || rel == "" {
        return "", contract.ErrPathInvalid
    }
    if filepath.IsAbs(rel) {
        return "", contract.ErrPathInvalid
    }
    if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
        return "", contract.ErrPathInvalid
    }
    if vol := filepath.VolumeName(rel); vol != "" {
        return "", contract.ErrPathInvalid
    }
    return filepath.Join(w.root, rel), nil
}

func (w *FS) writeOverwrite(ctx context.Context, dest string, r io.Reader) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, w.permF)
	if err != nil {
		return err
	}
	// 确保及时关闭
	defer f.Close()

	bw := bufio.NewWriterSize(f, w.bufSize)
	if _, err := io.Copy(bw, readerWithCtx(ctx, r)); err != nil {
		return err
	}
	return bw.Flush()
}

func (w *FS) writeAtomic(ctx context.Context, dest string, r io.Reader) error {
    dir := filepath.Dir(dest)
    tmp, err := os.CreateTemp(dir, ".tmp-*")
    if err != nil {
        return err
    }
    tmpPath := tmp.Name()
    // 目标权限：尽量与期望一致
    _ = os.Chmod(tmpPath, w.permF)

	bw := bufio.NewWriterSize(tmp, w.bufSize)
	if _, err := io.Copy(bw, readerWithCtx(ctx, r)); err != nil {
		_ = bw.Flush()
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := bw.Flush(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
    if err := tmp.Close(); err != nil {
        _ = os.Remove(tmpPath)
        return err
    }
    // 平台特定的原子替换（或最佳努力）：
    if err := osReplace(tmpPath, dest); err != nil {
        _ = os.Remove(tmpPath)
        return err
    }
    // 最佳努力：在部分平台同步父目录，提升崩溃安全性
    _ = syncDir(dir)
    return nil
}

// readerWithCtx: 在每次 Read 前检查 ctx 是否已取消。
func readerWithCtx(ctx context.Context, r io.Reader) io.Reader {
	return &ctxReader{ctx: ctx, r: r}
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *ctxReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	default:
	}
	return cr.r.Read(p)
}
