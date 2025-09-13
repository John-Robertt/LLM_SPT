package linear

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"llmspt/pkg/contract"
)

// Options: 预留占位，线性装配无需配置。
type Options struct{}

type assembler struct{}

// New 从原样 JSON Options 创建线性装配器（当前忽略选项）。
func New(raw json.RawMessage) (contract.Assembler, error) {
	// 预留未来宽松度/策略扩展点；当前为无状态实现
	_ = raw
	return &assembler{}, nil
}

// Assemble 按 From 严格升序线性拼接 spans.Output；
// 发现 FileID 混入、逆序或重叠即返回 ErrSeqInvalid。
func (a *assembler) Assemble(ctx context.Context, fileID contract.FileID, spans []contract.SpanResult) (io.Reader, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(spans) == 0 {
		return strings.NewReader(""), nil
	}

	// 线性校验：同一 FileID、严格升序、无重叠、From<=To
	prevTo := spans[0].To
	if spans[0].FileID != fileID || spans[0].From > prevTo {
		return nil, contract.ErrSeqInvalid
	}
	for i := 1; i < len(spans); i++ {
		s := spans[i]
		if s.FileID != fileID || s.From > s.To {
			return nil, contract.ErrSeqInvalid
		}
		// 严格升序：当前起点必须 > 上一个终点（不允许接触重叠）
		if !(s.From > prevTo) {
			return nil, contract.ErrSeqInvalid
		}
		// 记录推进
		prevTo = s.To
	}

	// 零拷贝倾向：拼接多个只读字符串 reader
	rs := make([]io.Reader, 0, len(spans))
	for _, s := range spans {
		// 允许空 Output；不插入分隔符
		rs = append(rs, strings.NewReader(s.Output))
	}
	return io.MultiReader(rs...), nil
}

var _ contract.Assembler = (*assembler)(nil)
