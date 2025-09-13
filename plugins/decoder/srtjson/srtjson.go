package srtjson

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "llmspt/pkg/contract"
)

// Options: 预留占位，SRT 场景默认逐条 JSON（[{id:int,text:string}]）。
// 当前无配置；保留以便未来扩展宽松度/字段名映射等。
type Options struct{}

type decoder struct{}

// New 从原样 JSON Options 创建解码器（当前忽略选项）。
func New(raw json.RawMessage) (contract.Decoder, error) {
	var opts Options
	// 保留解析点：未来可在此解析宽松选项（当前忽略解析错误）
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &opts)
	}
	return &decoder{}, nil
}

// 期望 Raw.Text 为严格 JSON 数组：[{"id": number, "text": string}, ...]
// 输出按 [i,i] 逐条对齐的 SpanResult 切片（顺序为 id 升序）。
func (d *decoder) Decode(ctx context.Context, tgt contract.Target, raw contract.Raw) ([]contract.SpanResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
    type item struct {
        ID   int64             `json:"id"`
        Text string            `json:"text"`
        Meta map[string]string `json:"meta,omitempty"`
    }
    var arr []item
    if err := json.Unmarshal([]byte(raw.Text), &arr); err != nil {
        // 将解析错误归类为响应无效
        return nil, fmt.Errorf("decode json per-record: %w", contract.ErrResponseInvalid)
    }
    // 空文本视为协议无效（失败）
    for _, it := range arr {
        if strings.TrimSpace(it.Text) == "" {
            return nil, fmt.Errorf("empty text for id %d: %w", it.ID, contract.ErrResponseInvalid)
        }
    }
    cands := make([]contract.SpanCandidate, 0, len(arr))
    for _, it := range arr {
        var m contract.Meta
        if len(it.Meta) > 0 {
            m = contract.Meta(it.Meta)
        }
        // 将纯译文放入 meta["dst_text"] 供边车优先使用
        if m == nil {
            m = make(contract.Meta, 1)
        } else {
            mm := make(contract.Meta, len(m)+1)
            for k, v := range m { mm[k] = v }
            m = mm
        }
        m["dst_text"] = it.Text
        cands = append(cands, contract.SpanCandidate{From: contract.Index(it.ID), To: contract.Index(it.ID), Output: it.Text, Meta: m})
    }
	spans, err := contract.ValidatePerRecord(tgt, cands)
	if err != nil {
		return nil, err
	}
	// 将 seq/time 渲染进 Output，形成完整 SRT 块文本；在装配层仅线性拼接
	for i := range spans {
		spans[i].Output = formatSRTBlock(spans[i].Meta, spans[i].Output)
		// 可选：清空 Meta 以减少后续耦合
		// spans[i].Meta = nil
	}
	return spans, nil
}

var _ contract.Decoder = (*decoder)(nil)

// DecodeWithMeta: 可选扩展——当上游未返回 meta 时，利用 idxMeta 回填。
func (d *decoder) DecodeWithMeta(ctx context.Context, tgt contract.Target, raw contract.Raw, idxMeta contract.IndexMetaMap) ([]contract.SpanResult, error) {
	// 复用 Decode 的解析逻辑
	type item struct {
		ID   int64             `json:"id"`
		Text string            `json:"text"`
		Meta map[string]string `json:"meta,omitempty"`
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
    var arr []item
    if err := json.Unmarshal([]byte(raw.Text), &arr); err != nil {
        return nil, fmt.Errorf("decode json per-record: %w", contract.ErrResponseInvalid)
    }
    // 空文本直接视为协议无效（失败）；不做任何回退
    for _, it := range arr {
        if strings.TrimSpace(it.Text) == "" {
            return nil, fmt.Errorf("empty text for id %d: %w", it.ID, contract.ErrResponseInvalid)
        }
    }
    // 检测可疑的“原文回显”：当上游对所有目标 id 的输出与源文本完全一致（在去首尾空白后）时，视为协议违例。
    // 注意：不做内容级回退，由上层决定如何处理。
    if len(arr) > 0 && idxMeta != nil {
        echo := true
        for _, it := range arr {
            src := ""
            if mm, ok := idxMeta[contract.Index(it.ID)]; ok {
                if t, ok2 := mm["_src_text"]; ok2 {
                    src = t
                }
            }
            if strings.TrimSpace(src) == "" || strings.TrimSpace(src) != strings.TrimSpace(it.Text) {
                echo = false
                break
            }
        }
        if echo {
            return nil, fmt.Errorf("echoed original detected: %w", contract.ErrResponseInvalid)
        }
    }
    cands := make([]contract.SpanCandidate, 0, len(arr))
    for _, it := range arr {
        var m contract.Meta
        if len(it.Meta) > 0 {
            m = contract.Meta(it.Meta)
        } else if idxMeta != nil {
            if mm, ok := idxMeta[contract.Index(it.ID)]; ok && len(mm) > 0 {
                // 拷贝一份，避免共享
                mm2 := make(contract.Meta, len(mm))
                for k, v := range mm {
                    mm2[k] = v
                }
                m = mm2
            }
        }
        // 将纯译文放入 meta["dst_text"] 供边车优先使用
        if m == nil {
            m = make(contract.Meta, 1)
        } else {
            mm := make(contract.Meta, len(m)+1)
            for k, v := range m { mm[k] = v }
            m = mm
        }
        m["dst_text"] = it.Text
        cands = append(cands, contract.SpanCandidate{From: contract.Index(it.ID), To: contract.Index(it.ID), Output: it.Text, Meta: m})
    }
	spans, err := contract.ValidatePerRecord(tgt, cands)
	if err != nil {
		return nil, err
	}
	for i := range spans {
		spans[i].Output = formatSRTBlock(spans[i].Meta, spans[i].Output)
		// spans[i].Meta = nil
	}
	return spans, nil
}

var _ contract.DecoderWithMeta = (*decoder)(nil)

// formatSRTBlock 将单条 span 渲染为 SRT 块文本：
// - 若 meta 中存在 "seq"/"time"，按行输出；
// - 追加文本行；
// - 以一个空行分隔（结尾包含 "\n\n"）。
func formatSRTBlock(meta contract.Meta, text string) string {
	// 预估容量：seq+time+text + 分隔
	// 简化实现，直接构造
	out := ""
	if meta != nil {
		if v := meta["seq"]; v != "" {
			out += v + "\n"
		}
		if v := meta["time"]; v != "" {
			out += v + "\n"
		}
	}
	if text != "" {
		out += text + "\n"
	}
	// 块分隔空行
	out += "\n"
	return out
}
