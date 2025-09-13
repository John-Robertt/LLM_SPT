package filesystem

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"llmspt/pkg/contract"
)

// Options 为 FileSystem Reader 的可选配置（最小必要）。
type Options struct {
	// BufSize 为读缓冲区大小（字节）。默认 64KiB。
	BufSize int `json:"buf_size"`
	// ExcludeDirNames: 在扫描目录时跳过这些目录名（基名完全匹配）。
	// 例如 [".git","node_modules","vendor"]。
	// 仅影响目录递归，不影响单文件 root。
	ExcludeDirNames []string `json:"exclude_dir_names"`
}

// FileSystem 实现基于文件系统与 STDIN 的 Reader。
// 行为遵循 architecture.md 第 3.1 节约束说明。
type FileSystem struct {
	bufSize int
	// 以小写形式保存，比较时按小写基名匹配。
	excludeDir map[string]struct{}
}

// New 创建 FileSystem Reader。
func New(opts *Options) *FileSystem {
	const defaultBuf = 64 * 1024
	b := defaultBuf
	if opts != nil && opts.BufSize > 0 {
		b = opts.BufSize
	}
	ex := make(map[string]struct{})
	if opts != nil && len(opts.ExcludeDirNames) > 0 {
		for _, name := range opts.ExcludeDirNames {
			if name == "" {
				continue
			}
			// 小写基名匹配，调用方无需关心大小写与前后斜杠。
			ex[strings.ToLower(name)] = struct{}{}
		}
	}
	return &FileSystem{bufSize: b, excludeDir: ex}
}

// Iterate 遍历 roots，按稳定顺序对每个常规文件调用 yield。
// 支持 roots 为空或仅包含 "-" 作为 STDIN。
func (r *FileSystem) Iterate(ctx context.Context, roots []string, yield func(fileID contract.FileID, rc io.ReadCloser) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if len(roots) == 0 || (len(roots) == 1 && roots[0] == "-") {
		// 统一缓冲策略：STDIN 也使用 bufio.Reader 封装
		return yield(contract.FileID("stdin"), newBufferedCloser(os.Stdin, r.bufSize))
	}
	// 禁止与其他根混用 "-"
	if len(roots) > 1 {
		for _, s := range roots {
			if s == "-" {
				return errors.New("stdin '-' cannot be mixed with other roots")
			}
		}
	}

	for _, root := range roots {
		if err := r.iterateOne(ctx, root, yield); err != nil {
			return err
		}
	}
	return nil
}

func (r *FileSystem) iterateOne(ctx context.Context, root string, yield func(contract.FileID, io.ReadCloser) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	// 仅跟随到常规文件；目录符号链接不跟随（忽略）
	if info.Mode()&os.ModeSymlink != 0 {
		t, err := os.Stat(root)
		if err != nil {
			return err
		}
		if t.Mode().IsRegular() {
			f, err := os.Open(root)
			if err != nil {
				return err
			}
			brc := newBufferedCloser(f, r.bufSize)
			if err := yield(contract.NormalizeFileID(root), brc); err != nil {
				_ = brc.Close()
				return err
			}
			return nil
		}
		// 非常规目标（含目录）：忽略，不报错
		return nil
	}

	if info.IsDir() {
		return r.walkDir(ctx, root, yield)
	}
	if !info.Mode().IsRegular() { // 跳过非常规文件
		return nil
	}
	f, err := os.Open(root)
	if err != nil {
		return err
	}
	brc := newBufferedCloser(f, r.bufSize)
	if err := yield(contract.NormalizeFileID(root), brc); err != nil {
		_ = brc.Close()
		return err
	}
	return nil
}

func (r *FileSystem) walkDir(ctx context.Context, dir string, yield func(contract.FileID, io.ReadCloser) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	// 稳定顺序：字典序
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	// 先目录（不跟随目录符号链接）
	for _, e := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if e.IsDir() {
			// 跳过指定目录名
			if _, skip := r.excludeDir[strings.ToLower(e.Name())]; skip {
				continue
			}
			if err := r.walkDir(ctx, filepath.Join(dir, e.Name()), yield); err != nil {
				return err
			}
		}
	}
	// 再文件（允许指向常规文件的符号链接；目录符号链接忽略）
	for _, e := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		// 判断符号链接目标
		if e.Type()&os.ModeSymlink != 0 {
			t, err := os.Stat(p)
			if err != nil {
				return err
			}
			if !t.Mode().IsRegular() {
				// 目标不是常规文件（如目录等）则忽略
				continue
			}
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			// 非常规且不是符号链接（如设备等）跳过
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		brc := newBufferedCloser(f, r.bufSize)
		if err := yield(contract.NormalizeFileID(p), brc); err != nil {
			_ = brc.Close()
			return err
		}
	}
	return nil
}

// bufferedCloser 将 bufio.Reader 与底层 Closer 组合为 ReadCloser。
type bufferedCloser struct {
	*bufio.Reader
	c io.Closer
}

func newBufferedCloser(c io.ReadCloser, bufSize int) *bufferedCloser {
	if bufSize <= 0 {
		bufSize = 64 * 1024
	}
	return &bufferedCloser{Reader: bufio.NewReaderSize(c, bufSize), c: c}
}

func (b *bufferedCloser) Close() error { return b.c.Close() }
