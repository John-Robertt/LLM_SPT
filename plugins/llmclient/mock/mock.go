package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"llmspt/pkg/contract"
)

// Options: 最小调试配置（可选）。
type Options struct {
    Prefix string `json:"prefix"` // 输出前缀，默认 "MOCK"
	// APIKey: 仅用于限流分组（调试用），默认使用内置常量，不参与任何网络请求。
	APIKey string `json:"api_key"`
	// ResponseMode: 可选的响应模式（用于集成测试与无网络联调）。
    //  - "": 留空或未知值时，默认使用 "translate_json_per_record"（与 srtjson 解码器即插即用）。
    //  - "translate_json_per_record": 按 Batch.TargetFrom..To 产出严格 JSON 数组 [{id:int,text:string}]，text 为对应 Record.Text 的占位翻译（前缀 Prefix）。
    //    现在额外包含可选 meta 字段（若源 Record.Meta 存在），形如 meta:{key:string}，便于下游解码器透传。
    //  - "translate_json_span": 产出 {from:int,to:int,text:string}，text 为 Target 文本拼接（每条以 \n 连接）。
    //  - "line_map": 按行映射 Target 文本，直接返回多行文本。
    ResponseMode string `json:"response_mode,omitempty"`
}

type Client struct {
	prefix string
	mode   string
}

func New(raw json.RawMessage) (contract.LLMClient, error) {
    var o Options
    if len(raw) > 0 {
        _ = json.Unmarshal(raw, &o)
    }
    if o.Prefix == "" {
        o.Prefix = "MOCK"
    }
    if o.APIKey == "" {
        o.APIKey = "MOCK_DEBUG_KEY"
    }
    mode := o.ResponseMode
    if strings.TrimSpace(mode) == "" {
        // 新默认：逐条 JSON，便于与 srtjson 解码器直接联调
        mode = "translate_json_per_record"
    }
    return &Client{prefix: o.Prefix, mode: mode}, nil
}

func (c *Client) Invoke(ctx context.Context, b contract.Batch, p contract.Prompt) (contract.Raw, error) {
	// 仅用于模块/流程调试：把 Prompt 原样或简化回显为 Raw。
	switch c.mode {
	case "translate_json_per_record":
		// 逐条 JSON：根据 Target 索引构造占位翻译，text = Prefix + ":" + 原文
		// 若源 Record.Meta 存在，则附带 meta 对象，便于解码器透传至 SpanResult.Meta。
		type item struct {
			ID   int64             `json:"id"`
			Text string            `json:"text"`
			Meta map[string]string `json:"meta,omitempty"`
		}
		items := make([]item, 0, int(b.TargetTo-b.TargetFrom+1))
		base := b.Records[0].Index
		for idx := b.TargetFrom; idx <= b.TargetTo; idx++ {
			off := int(idx - base)
			if off < 0 || off >= len(b.Records) {
				return contract.Raw{}, fmt.Errorf("mock: index out of window: %d", idx)
			}
			rec := b.Records[off]
			it := item{ID: int64(idx), Text: fmt.Sprintf("%s: %s", c.prefix, rec.Text)}
			if len(rec.Meta) > 0 {
				m := make(map[string]string, len(rec.Meta))
				for k, v := range rec.Meta {
					m[k] = v
				}
				it.Meta = m
			}
			items = append(items, it)
		}
		bts, _ := json.Marshal(items)
		return contract.Raw{Text: string(bts)}, nil
	case "translate_json_span":
		// 整段 JSON：from/to + 拼接文本
		var lines []string
		base := b.Records[0].Index
		for idx := b.TargetFrom; idx <= b.TargetTo; idx++ {
			off := int(idx - base)
			if off < 0 || off >= len(b.Records) {
				return contract.Raw{}, fmt.Errorf("mock: index out of window: %d", idx)
			}
			lines = append(lines, b.Records[off].Text)
		}
		obj := struct {
			From, To int64
			Text     string
		}{From: int64(b.TargetFrom), To: int64(b.TargetTo), Text: strings.Join(lines, "\n")}
		bts, _ := json.Marshal(obj)
		return contract.Raw{Text: string(bts)}, nil
	case "line_map":
		// 多行文本：每个目标索引一行
		base := b.Records[0].Index
		var sb strings.Builder
		for idx := b.TargetFrom; idx <= b.TargetTo; idx++ {
			off := int(idx - base)
			if off < 0 || off >= len(b.Records) {
				return contract.Raw{}, fmt.Errorf("mock: index out of window: %d", idx)
			}
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(b.Records[off].Text)
		}
		return contract.Raw{Text: sb.String()}, nil
	}

    // 兜底：回显 Prompt 摘要（仅当显式配置了未知模式时触发）
    switch v := p.(type) {
    case contract.TextPrompt:
        return contract.Raw{Text: fmt.Sprintf("%s(text): %s", c.prefix, string(v))}, nil
    case contract.ChatPrompt:
        if len(v) == 0 {
            return contract.Raw{Text: fmt.Sprintf("%s(chat): <empty>", c.prefix)}, nil
        }
        // 取第一条消息内容，避免打印过长
        return contract.Raw{Text: fmt.Sprintf("%s(chat:%s): %s", c.prefix, v[0].Role, v[0].Content)}, nil
    default:
        return contract.Raw{Text: fmt.Sprintf("%s(unknown prompt type)", c.prefix)}, nil
    }
}
