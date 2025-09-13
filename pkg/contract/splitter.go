package contract

import (
	"context"
	"io"
)

// Splitter: 将单文件字节流拆分为有序 Record 序列，并分配 Index（0..n-1）。
// 约束：
// 1) 不跨文件合并；
// 2) Index 严格递增且稳定；
// 3) 不改变文本语义（仅做 CRLF→LF 的最小必要归一）；
// 4) 无内部并发、幂等；
// 5) 尺寸上界仅以字节/字符为单位（若启用）。
type Splitter interface {
	Split(ctx context.Context, fileID FileID, r io.Reader) ([]Record, error)
}
