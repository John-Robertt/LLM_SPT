package sliding

import (
	"context"
	"errors"
	"fmt"

	"llmspt/pkg/contract"
)

// Options 为滑动窗口 Batcher 的可选配置（最小必要）。
type Options struct {
    // ContextRadius: 上下文半径（左右各 ContextRadius 条）。< 0 视为 0。
    // 为提升可读性，替代原先的简写 C。
    ContextRadius int `json:"context_radius"`
    // BytesPerToken: 估算系数，tokens ≈ ceil(utf8_bytes / BytesPerToken)。
    // 典型默认值为 4。<=0 时采用默认 4。
    BytesPerToken int `json:"bytes_per_token"`
    // ExtraBytesPerRecord: 每条记录在 Prompt 包装产生的额外字节估算（如 <seg id> 包裹、换行、targets 等）。
    // 仅用于预算估算，不影响实际内容；<=0 表示不额外加成。
    ExtraBytesPerRecord int `json:"extra_bytes_per_record"`
}

// Batcher 实现滑动窗口批处理与上下文窗口。
type Batcher struct {
    ctxRadius     int
    bytesPerToken int
    extraPerRec   int
}

// New 创建滑动窗口 Batcher。
func New(opts *Options) *Batcher {
    r := 0
    bpt := 4
    extra := 0
    if opts != nil {
        if opts.ContextRadius > 0 {
            r = opts.ContextRadius
        }
        if opts.BytesPerToken > 0 {
            bpt = opts.BytesPerToken
        }
        if opts.ExtraBytesPerRecord > 0 {
            extra = opts.ExtraBytesPerRecord
        }
    }
    return &Batcher{ctxRadius: r, bytesPerToken: bpt, extraPerRec: extra}
}

// Make 实现 3.3 的滑动窗口批处理：
// - 同一 FileID 内按 Index 连续切片；
// - 批内排列为 [L 上下文][Target][R 上下文]；
// - 仅 Target 区间参与最终装配；
// - 使用简单的 token 估算与前缀和在 O(n) 时间内完成。
func (b *Batcher) Make(ctx context.Context, records []contract.Record, limit contract.BatchLimit) ([]contract.Batch, error) {
	if limit.MaxTokens <= 0 {
		return nil, errors.New("batcher: max tokens must be > 0")
	}
	n := len(records)
	if n == 0 {
		return nil, nil
	}
	// 校验 FileID 一致与 Index 连续（0..n-1）。
	fid := records[0].FileID
	if records[0].Index != 0 {
		return nil, fmt.Errorf("batcher: first index must be 0, got %d", records[0].Index)
	}
	for i := 1; i < n; i++ {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		if records[i].FileID != fid {
			return nil, errors.New("batcher: records must have the same FileID")
		}
		if records[i].Index != records[i-1].Index+1 {
			return nil, errors.New("batcher: record Index must be contiguous and strictly increasing")
		}
	}

	// 估算每条记录的 token 数并直接构建前缀和（去除多余 tok 切片）。
	pref := make([]int, n+1)
	for i := 0; i < n; i++ {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		t := b.estimateTokens(records[i].Text)
		pref[i+1] = pref[i] + t
	}

	// 有效预算。
	budget := limit.MaxTokens
	if budget <= 0 {
		return nil, errors.New("batcher: effective token budget must be > 0")
	}

	var batches []contract.Batch
	var batchIdx int64 = 0
	l := 0 // 目标区间左端（包含）
	for l < n {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		L1 := l - b.ctxRadius
		if L1 < 0 {
			L1 = 0
		}
		L2 := l - 1
		// 扩展目标区间的右端 r（半开区间），直到无法再容纳。
		r := l
		bestR := l // 记录“最后一次可容纳且含至少一个目标”的 r 值
		for r <= n {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			R1 := r
			R2 := r + b.ctxRadius - 1
			if R2 >= n {
				R2 = n - 1
			}
			need := sum(pref, L1, L2) + sum(pref, l, r-1) + sum(pref, R1, R2)
			if need <= budget {
				// 只有当至少包含 1 个目标（r>l）时，才更新 bestR。
				if r > l {
					bestR = r
				}
				r++
			} else {
				break
			}
		}
		if bestR == l { // 连 1 条目标也放不下
			return nil, errors.New("batcher: single target with contexts does not fit; decrease C or split")
		}
		// 依据最终 bestR 计算右上下文上界 R2，并发出批。
		R2 := bestR + b.ctxRadius - 1
		if R2 >= n {
			R2 = n - 1
		}
		recSlice := records[L1 : R2+1]
		batches = append(batches, contract.Batch{
			FileID:     fid,
			BatchIndex: batchIdx,
			Records:    recSlice,
			TargetFrom: records[l].Index,
			TargetTo:   records[bestR-1].Index,
		})
		batchIdx++
		l = bestR
	}
	return batches, nil
}

// estimateTokens: 近似估算 tokens ≈ ceil(utf8_bytes / bytesPerToken)。
func (b *Batcher) estimateTokens(s string) int {
    // 使用字节长度（避免遍历 rune），保证 O(1) 开销。
    bytes := len(s)
    // 估算每条记录的包装额外字节（如 <seg id> 包裹/换行/targets 等）
    if b.extraPerRec > 0 {
        bytes += b.extraPerRec
    }
    if bytes == 0 {
        return 0
    }
    d := b.bytesPerToken
    if d <= 0 {
        d = 4
    }
    // ceil(bytes / d)
    return (bytes + d - 1) / d
}

func sum(pref []int, a, b int) int {
	if a > b {
		return 0
	}
	if a < 0 {
		a = 0
	}
	if b+1 >= len(pref) {
		b = len(pref) - 2
	}
	return pref[b+1] - pref[a]
}

func ctxErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// 静态依赖，确保本包被引用时不会被 Go 工具链误删（如通过 registry 引用）。
var _ contract.Batcher = (*Batcher)(nil)
