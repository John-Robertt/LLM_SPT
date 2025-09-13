package contract

import (
	"context"
	"io"
)

// Assembler: 基于 SpanResult 的 From/To 线性装配为最终文本（单文件）。
// 约束：
//  1. 仅对同一 FileID 的 Span 进行装配；
//  2. 按 From 严格升序拼接；
//  3. 同批内区间不得重叠；
//  4. 不引入跨文件状态；
//  5. 序列违规返回 ErrSeqInvalid。
type Assembler interface {
	Assemble(ctx context.Context, fileID FileID, spans []SpanResult) (io.Reader, error)
}
