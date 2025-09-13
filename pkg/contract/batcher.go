package contract

import "context"

// BatchLimit: 最小必要限制集合（滑动窗口模型）。
// 仅包含与“是否可装入批”直接相关的上限参数。
type BatchLimit struct {
	// MaxTokens: 每批最大 token 预算（近似估算）。
	// 必须为正数。
	MaxTokens int
}

// Batcher: 将同一 FileID 的有序 Record 切分为若干 Batch。
// 约束：
//  1. 仅在同一 FileID 内成批；
//  2. 遵循 token 上限与固定上下文条数（由实现配置提供）；
//  3. 不重排、不丢失；批内排列为 [L 上下文][Target][R 上下文]；
//  4. 每个 Batch 必须设置 TargetFrom/TargetTo（闭区间，基于全局 Index）。
//  5. 建议实现为每个 Batch 赋予单调递增的 BatchIndex（同一 FileID 内 0..n-1），用于跨批顺序恢复。
type Batcher interface {
	Make(ctx context.Context, records []Record, limit BatchLimit) ([]Batch, error)
}
