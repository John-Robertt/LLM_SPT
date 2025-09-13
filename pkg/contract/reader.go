package contract

import (
	"context"
	"io"
)

// Reader: 输入源抽象（文件/目录/STDIN）。
// 约束：
// 1) 流式读取，按文件维度回调；
// 2) FileID 稳定且去平台差异化；
// 3) 不做解码/业务解析，仅提供字节流；
// 4) 不在内部起并发。
type Reader interface {
	Iterate(ctx context.Context, roots []string, yield func(fileID FileID, r io.ReadCloser) error) error
}
