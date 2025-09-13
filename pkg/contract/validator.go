package contract

import "context"

// Target: 目标区间最小载体（等价于 Batch 的 TargetFrom/TargetTo 只读视图）。
type Target struct {
	FileID FileID
	From   Index
	To     Index
}

// SpanResult: 统一结果 IR（同一 FileID 内、非重叠、按 From 严格升序）。
type SpanResult struct {
	FileID FileID
	From   Index
	To     Index
	Output string
	// Meta: 可选的轻量元信息（例如还原结构化容器字段：SRT 序号/时间轴等）。
	// Assembler 可按需使用；为空表示无。
	Meta Meta
}

// SpanCandidate: 解码中间态（尚未绑定 FileID），供校验库函数使用。
type SpanCandidate struct {
	From   Index
	To     Index
	Output string
	Meta   Meta
}

// IndexMetaMap: 只读视图（按约定使用）——将全局 Index 映射到源 Record 的 Meta。
// 解码器可使用该映射在缺少上游 meta 的情况下回填必要的元信息。
// 注意：调用方应仅传入当前批窗口内可见的条目；不要求覆盖 target 以外索引。
type IndexMetaMap map[Index]Meta

// 校验库函数（纯函数，无 I/O）：
// - ValidatePerRecord: 逐条对齐，要求 cands 为 [i,i] 且连续覆盖 [tgt.From..tgt.To]
// - ValidateWhole:    整段对齐，要求单个区间恰为 [tgt.From,tgt.To]
func ValidatePerRecord(tgt Target, cands []SpanCandidate) ([]SpanResult, error) {
	if tgt.From > tgt.To {
		return nil, ErrInvalidInput
	}
	need := int(tgt.To - tgt.From + 1)
	if len(cands) != need {
		return nil, ErrResponseInvalid
	}
	spans := make([]SpanResult, 0, len(cands))
	expect := tgt.From
	for i := 0; i < len(cands); i++ {
		c := cands[i]
		if c.From > c.To { // 单点区间必须相等
			return nil, ErrResponseInvalid
		}
		if c.From != c.To {
			return nil, ErrResponseInvalid
		}
		if c.From != expect {
			return nil, ErrResponseInvalid
		}
		spans = append(spans, SpanResult{FileID: tgt.FileID, From: c.From, To: c.To, Output: cloneString(c.Output), Meta: cloneMeta(c.Meta)})
		expect++
	}
	if expect != tgt.To+1 {
		return nil, ErrResponseInvalid
	}
	return spans, nil
}

func ValidateWhole(tgt Target, cands []SpanCandidate) ([]SpanResult, error) {
	if tgt.From > tgt.To {
		return nil, ErrInvalidInput
	}
	if len(cands) != 1 {
		return nil, ErrResponseInvalid
	}
	c := cands[0]
	if c.From != tgt.From || c.To != tgt.To {
		return nil, ErrResponseInvalid
	}
	return []SpanResult{{FileID: tgt.FileID, From: c.From, To: c.To, Output: cloneString(c.Output), Meta: cloneMeta(c.Meta)}}, nil
}

// Decoder: 将 Raw 解码并返回最终 []SpanResult；字段名/格式/回退策略由具体实现自决。
// 解码策略属于业务/编排层扩展，架构仅定义协议。
type Decoder interface {
	Decode(ctx context.Context, tgt Target, raw Raw) ([]SpanResult, error)
}

// DecoderWithMeta: 可选扩展接口。若实现该接口，编排层可传入 Index→Meta 的只读映射，
// 以便解码器在上游未返回 meta 时进行回填，生成带 Meta 的 SpanResult。
type DecoderWithMeta interface {
	DecodeWithMeta(ctx context.Context, tgt Target, raw Raw, idxMeta IndexMetaMap) ([]SpanResult, error)
}

// cloneString: 强制拷贝字符串，避免底层共享导致生命周期耦合。
func cloneString(s string) string {
	if s == "" {
		return ""
	}
	b := make([]byte, len(s))
	copy(b, s)
	return string(b)
}

// cloneMeta: 复制 Meta 映射，避免引用共享导致意外修改。
func cloneMeta(m Meta) Meta {
	if m == nil {
		return nil
	}
	out := make(Meta, len(m))
	for k, v := range m {
		// 强制拷贝 k/v 字符串
		out[cloneString(k)] = cloneString(v)
	}
	return out
}
