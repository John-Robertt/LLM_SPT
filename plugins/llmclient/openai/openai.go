package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"llmspt/pkg/contract"
)

// Options: 最小必需配置。
type Options struct {
	BaseURL        string   `json:"base_url"`        // 例如 https://api.openai.com/v1
	Model          string   `json:"model"`           // 为空则使用默认
	APIKeyEnv      string   `json:"api_key_env"`     // 优先从环境变量读取
	APIKey         string   `json:"api_key"`         // 明文传入（不推荐，按需用于测试）
    TimeoutSeconds int      `json:"timeout_seconds"` // 可选 client 级超时（秒）
	Temperature    *float64 `json:"temperature,omitempty"`
	// 第三方兼容（最小）：
	EndpointPath       string            `json:"endpoint_path"`        // 覆盖默认 /chat/completions；可为完整 URL（以 http 开头）
	DisableDefaultAuth bool              `json:"disable_default_auth"` // 关闭默认 Authorization: Bearer 注入
	ExtraHeaders       map[string]string `json:"extra_headers"`        // 追加/覆盖请求头（用于 OpenAI 兼容服务，如 Azure/OpenRouter 等）
}

func (o *Options) defaults() {
	if o.BaseURL == "" {
		o.BaseURL = "https://api.openai.com/v1"
	}
	if o.Model == "" {
		// 选择一个轻量、价格友好的默认模型
		o.Model = "gpt-4.1-mini"
	}
	if o.APIKeyEnv == "" {
		o.APIKeyEnv = "OPENAI_API_KEY"
	}
	if o.EndpointPath == "" {
		o.EndpointPath = "/chat/completions"
	}
}

type Client struct {
	hc          *http.Client
	url         string
	apiKey      string
	temp        *float64
	model       string
	extraH      map[string]string
	disableAuth bool
	do          func(*http.Request) (*http.Response, error)
}

// New 从原样 JSON 选项构造客户端。
func New(raw json.RawMessage) (contract.LLMClient, error) {
	var opts Options
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &opts); err != nil {
			return nil, fmt.Errorf("openai options: %w", err)
		}
	}
	opts.defaults()
	key := opts.APIKey
	if key == "" && opts.APIKeyEnv != "" {
		key = os.Getenv(opts.APIKeyEnv)
	}
	if key == "" {
		return nil, fmt.Errorf("openai: %w: missing api key", contract.ErrInvalidInput)
	}
    // 设置 HTTP 客户端超时：未配置则采用安全默认 60s
    if opts.TimeoutSeconds <= 0 {
        opts.TimeoutSeconds = 60
    }
    hc := &http.Client{Timeout: time.Duration(opts.TimeoutSeconds) * time.Second}
	// 解析 URL：允许 endpoint_path 为完整 URL
	fullURL := opts.EndpointPath
	if !(strings.HasPrefix(fullURL, "http://") || strings.HasPrefix(fullURL, "https://")) {
		// 健壮拼接，确保恰好一个斜杠
		base := strings.TrimRight(opts.BaseURL, "/")
		path := strings.TrimLeft(opts.EndpointPath, "/")
		fullURL = base + "/" + path
	}
	return &Client{
		hc:          hc,
		url:         fullURL,
		apiKey:      key,
		temp:        opts.Temperature,
		model:       opts.Model,
		extraH:      opts.ExtraHeaders,
		disableAuth: opts.DisableDefaultAuth,
		do:          hc.Do,
	}, nil
}

type oaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type oaReq struct {
    Model       string      `json:"model"`
    Messages    []oaMessage `json:"messages"`
    Temperature *float64    `json:"temperature,omitempty"`
    ResponseFormat *oaResponseFormat `json:"response_format,omitempty"`
}
type oaResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// OpenAI response_format for JSON modes (minimal subset).
// When schema is present in Prompt, we set type=json_schema to enforce structured JSON.
type oaResponseFormat struct {
    Type       string        `json:"type"` // "json_object" or "json_schema"
    JSONSchema *oaJSONSchema `json:"json_schema,omitempty"`
}

type oaJSONSchema struct {
    Name   string          `json:"name"`
    Schema json.RawMessage `json:"schema"`
    Strict bool            `json:"strict,omitempty"`
}

// upstreamError 实现 net.Error，用于将 HTTP 上游 5xx/408 映射为网络类错误，便于分类。
type upstreamError struct{ status int; msg string }

func (e upstreamError) Error() string { return fmt.Sprintf("openai upstream %d: %s", e.status, e.msg) }
func (e upstreamError) Timeout() bool { return e.status == http.StatusRequestTimeout }
func (e upstreamError) Temporary() bool { return e.status/100 == 5 }
func (e upstreamError) UpstreamStatus() int { return e.status }
func (e upstreamError) UpstreamMessage() string { return e.msg }

// extractJSONSchemaFromPrompt: 若 Prompt 中包含一条 role=="json_schema" 的消息，解析其 Content 为 JSON 并返回 schema，且从对话中移除此消息。
// 与 Gemini 实现保持一致；未找到或解析失败则返回原 Prompt 与空 schema。
func extractJSONSchemaFromPrompt(p contract.Prompt) (contract.Prompt, json.RawMessage) {
    cp, ok := p.(contract.ChatPrompt)
    if !ok {
        return p, nil
    }
    out := make(contract.ChatPrompt, 0, len(cp))
    var schema json.RawMessage
    for _, m := range cp {
        if strings.EqualFold(strings.TrimSpace(m.Role), "json_schema") {
            var raw json.RawMessage
            if json.Unmarshal([]byte(m.Content), &raw) == nil && len(raw) > 0 {
                schema = raw
            }
            continue
        }
        out = append(out, m)
    }
    return out, schema
}

func (c *Client) encodePrompt(p contract.Prompt, model string, rf *oaResponseFormat) ([]byte, error) {
    var req oaReq
    req.Model = model
    req.Temperature = c.temp
    switch v := p.(type) {
    case contract.TextPrompt:
        req.Messages = []oaMessage{{Role: "user", Content: string(v)}}
    case contract.ChatPrompt:
        req.Messages = make([]oaMessage, 0, len(v))
        for _, m := range v {
            // 跳过用于 Gemini 的 schema 携带消息
            if strings.EqualFold(strings.TrimSpace(m.Role), "json_schema") {
                continue
            }
            req.Messages = append(req.Messages, oaMessage{Role: m.Role, Content: m.Content})
        }
    default:
        return nil, contract.ErrInvalidInput
    }
    if rf != nil {
        req.ResponseFormat = rf
    }
    return json.Marshal(&req)
}

// Invoke: 单次调用，同步返回。
func (c *Client) Invoke(ctx context.Context, b contract.Batch, p contract.Prompt) (contract.Raw, error) {
	// 提取模型：允许通过 Prompt 的 Meta/类型携带，但按“最小必需”不做读取；统一使用默认/Options 中的模型。
	// 为了避免在 Client 中保存模型，我们从 encode 中编码。
	// 这里读取默认模型作为占位：
	model := c.model
	if model == "" {
		model = "gpt-4.1-mini"
	}
    // 从 Prompt 中抽取 JSON Schema；若存在则启用 OpenAI 的 json_schema 响应格式
    pp, schema := extractJSONSchemaFromPrompt(p)
    var rf *oaResponseFormat
    if len(schema) > 0 {
        rf = &oaResponseFormat{Type: "json_schema", JSONSchema: &oaJSONSchema{Name: "srtjson", Schema: schema, Strict: true}}
    }
    body, err := c.encodePrompt(pp, model, rf)
	if err != nil {
		if errors.Is(err, contract.ErrInvalidInput) {
			return contract.Raw{}, err
		}
		return contract.Raw{}, fmt.Errorf("encode: %v: %w", err, contract.ErrInvalidInput)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return contract.Raw{}, fmt.Errorf("new request: %v: %w", err, contract.ErrInvalidInput)
	}
	if !c.disableAuth {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.extraH {
		if k == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := c.do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return contract.Raw{}, ctx.Err()
		}
		return contract.Raw{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return contract.Raw{}, contract.ErrRateLimited
	}
	if resp.StatusCode/100 != 2 {
		// 读取少量响应体辅助定位
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		msg := strings.TrimSpace(string(slurp))
		// 分类：4xx 视为输入/配置无效；5xx 视为网络/上游问题；408 特判为网络
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode/100 == 5 {
			return contract.Raw{}, upstreamError{status: resp.StatusCode, msg: msg}
		}
		return contract.Raw{}, fmt.Errorf("openai upstream %d: %w", resp.StatusCode, contract.ErrInvalidInput)
	}
	var or oaResp
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&or); err != nil {
		return contract.Raw{}, fmt.Errorf("decode: %w", contract.ErrResponseInvalid)
	}
	if len(or.Choices) == 0 || or.Choices[0].Message.Content == "" {
		return contract.Raw{}, contract.ErrResponseInvalid
	}
	return contract.Raw{Text: or.Choices[0].Message.Content}, nil
}
