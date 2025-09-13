package contract

import (
	"context"
	"errors"
)

// Raw: LLM 客户端返回的原始文本载荷（万能容器）。
// 约束：原样返回，不做清洗/截断/归一化。
type Raw struct {
	Text string
}

// LLMClient: 以 Batch+Prompt 为单位与大模型交互，返回原始文本 Raw。
// 单次调用、同步返回；应尊重 ctx 取消/超时并及时释放资源。
type LLMClient interface {
	Invoke(ctx context.Context, b Batch, p Prompt) (Raw, error)
}

// 可选：流式接口（非核心契约）。
type LLMStreamer interface {
	InvokeStream(ctx context.Context, b Batch, p Prompt) (RawStream, error)
}

// RawStream: 只读顺序拉取；调用方负责在用毕后 Close。
type RawStream interface {
	Next() (chunk string, done bool, err error)
	Close() error
}

// 最小错误分类（用于上层策略判定）。
var (
	ErrRateLimited     = errors.New("rate limited")
	ErrResponseInvalid = errors.New("response invalid")
	ErrInvalidInput    = errors.New("invalid input")
	ErrSeqInvalid      = errors.New("sequence invalid")
)
