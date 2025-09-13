package flaky

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"

	"llmspt/pkg/contract"
)

// Options 定义可选项。
type Options struct {
	Prefix string `json:"prefix"`
	// LogPath: 调试用日志文件，记录每次调用结果（可选）。
	LogPath string `json:"log_path,omitempty"`
}

// Client 是带状态的 LLM 实现：
// 第一次 Invoke 返回 ErrRateLimited；
// 第二次返回无法解析的 JSON；
// 之后返回占位翻译 JSON。
type Client struct {
	prefix  string
	logPath string
	count   atomic.Int32
}

// New 构造 Client。
func New(raw json.RawMessage) (contract.LLMClient, error) {
	var o Options
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, err
		}
	}
	if o.Prefix == "" {
		o.Prefix = "FLAKY"
	}
	c := &Client{prefix: o.Prefix, logPath: o.LogPath}
	return c, nil
}

func (c *Client) log(s string) {
	if c.logPath == "" {
		return
	}
	// 追加写入，忽略错误。
	_ = appendFile(c.logPath, s+"\n")
}

// appendFile 以追加方式写入。
func appendFile(path, s string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}

// Invoke 实现 contract.LLMClient。
func (c *Client) Invoke(ctx context.Context, b contract.Batch, p contract.Prompt) (contract.Raw, error) {
	switch c.count.Add(1) {
	case 1:
		c.log("rate_limited")
		return contract.Raw{}, contract.ErrRateLimited
	case 2:
		c.log("invalid_json")
		return contract.Raw{Text: "invalid"}, nil
	default:
		c.log("ok")
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
				return contract.Raw{}, errors.New("flaky: index out of range")
			}
			rec := b.Records[off]
			it := item{ID: int64(idx), Text: c.prefix + ": " + rec.Text}
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
	}
}

var _ contract.LLMClient = (*Client)(nil)
