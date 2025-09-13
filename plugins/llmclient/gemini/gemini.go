package gemini

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"

    "llmspt/pkg/contract"
)

// Options: Google Generative Language API (Gemini) 最小必需。
type Options struct {
    BaseURL   string `json:"base_url"`    // https://generativelanguage.googleapis.com
    Model     string `json:"model"`       // 默认 gemini-1.5-flash
    APIKeyEnv string `json:"api_key_env"` // 默认 GOOGLE_API_KEY
    APIKey    string `json:"api_key"`
    // 客户端超时（秒）。未设置或 <=0 时采用默认 60 秒。
    TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// 第三方兼容（最小）
	EndpointPath  string            `json:"endpoint_path"`    // 可覆盖默认 /v1beta/models/{model}:generateContent；支持 {model} 占位
	APIKeyInQuery *bool             `json:"api_key_in_query"` // 默认 true；为 false 时使用 x-goog-api-key 头
	ExtraHeaders  map[string]string `json:"extra_headers"`
	ExtraQuery    map[string]string `json:"extra_query"`
	// JSON 输出 MIME（可选）：仅当 Prompt 携带 schema 时才会生效；为空则使用 application/json
	ResponseMIMEType string `json:"response_mime_type,omitempty"`
}

func (o *Options) defaults() {
	if o.BaseURL == "" {
		o.BaseURL = "https://generativelanguage.googleapis.com"
	}
	if o.Model == "" {
		o.Model = "gemini-2.5-flash"
	}
	if o.APIKeyEnv == "" {
		o.APIKeyEnv = "GOOGLE_API_KEY"
	}
	if o.EndpointPath == "" {
		o.EndpointPath = "/v1beta/models/{model}:generateContent"
	}
	// 默认把 key 放在 query（与官方 API 对齐）
	if o.APIKeyInQuery == nil {
		t := true
		o.APIKeyInQuery = &t
	}
}

type Client struct {
	hc      *http.Client
	url     string // 完整路径（包含模型路径或占位展开）
	apiKey  string
	inQuery bool
	extraH  map[string]string
	extraQ  map[string]string
	do      func(*http.Request) (*http.Response, error)
	// JSON 输出配置：MIME 可配置，Schema 改由 Prompt 携带
	respMIME string
}

func New(raw json.RawMessage) (contract.LLMClient, error) {
	var opts Options
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &opts); err != nil {
			return nil, err
		}
	}
	opts.defaults()
	key := opts.APIKey
	if key == "" && opts.APIKeyEnv != "" {
		key = os.Getenv(opts.APIKeyEnv)
	}
	if key == "" {
		return nil, fmt.Errorf("gemini: %w: missing api key", contract.ErrInvalidInput)
	}
	path := strings.ReplaceAll(opts.EndpointPath, "{model}", url.PathEscape(opts.Model))
	if !(strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")) {
		base := strings.TrimRight(opts.BaseURL, "/")
		p := strings.TrimLeft(path, "/")
		path = base + "/" + p
	}
	inQuery := true
	if opts.APIKeyInQuery != nil {
		inQuery = *opts.APIKeyInQuery
	}
    // 设置 HTTP 客户端超时：未配置则采用安全默认 60s
    if opts.TimeoutSeconds <= 0 {
        opts.TimeoutSeconds = 60
    }
    hc := &http.Client{Timeout: time.Duration(opts.TimeoutSeconds) * time.Second}
    return &Client{hc: hc, url: path, apiKey: key, inQuery: inQuery, extraH: opts.ExtraHeaders, extraQ: opts.ExtraQuery, do: hc.Do,
        respMIME: opts.ResponseMIMEType,
    }, nil
}

// 请求/响应（最小字段）。
type gmPart struct {
	Text string `json:"text"`
}
type gmContent struct {
	Role  string   `json:"role,omitempty"`
	Parts []gmPart `json:"parts"`
}
type gmGenerationConfig struct {
	ResponseMIMEType string          `json:"response_mime_type,omitempty"`
	ResponseSchema   json.RawMessage `json:"response_schema,omitempty"`
}
type gmReq struct {
	Contents         []gmContent         `json:"contents"`
	GenerationConfig *gmGenerationConfig `json:"generationConfig,omitempty"`
}
type gmResp struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// upstreamError 实现 net.Error，用于将 HTTP 上游 5xx/408 映射为网络类错误。
type upstreamError struct{ status int; msg string }

func (e upstreamError) Error() string { return fmt.Sprintf("gemini upstream %d: %s", e.status, e.msg) }
func (e upstreamError) Timeout() bool { return e.status == http.StatusRequestTimeout }
func (e upstreamError) Temporary() bool { return e.status/100 == 5 }
func (e upstreamError) UpstreamStatus() int { return e.status }
func (e upstreamError) UpstreamMessage() string { return e.msg }

// extractJSONSchemaFromPrompt: 若 Prompt 中包含一条 role=="json_schema" 的消息，解析其 Content 为 JSON 并返回 schema，且从对话中移除此消息。
// 若未找到或解析失败，则返回原 Prompt 与空 schema（解析失败视作无 schema，避免硬失败）。
func extractJSONSchemaFromPrompt(p contract.Prompt) (contract.Prompt, json.RawMessage) {
	cp, ok := p.(contract.ChatPrompt)
	if !ok {
		return p, nil
	}
	out := make(contract.ChatPrompt, 0, len(cp))
	var schema json.RawMessage
	for _, m := range cp {
		if strings.EqualFold(strings.TrimSpace(m.Role), "json_schema") {
			// 尝试解析 JSON；失败则忽略
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

func encodePrompt(p contract.Prompt, gc *gmGenerationConfig) ([]byte, error) {
	var req gmReq
	switch v := p.(type) {
	case contract.TextPrompt:
		req.Contents = []gmContent{{Role: "user", Parts: []gmPart{{Text: string(v)}}}}
	case contract.ChatPrompt:
		req.Contents = make([]gmContent, 0, len(v))
		for _, m := range v {
			role := normalizeGeminiRole(m.Role)
			req.Contents = append(req.Contents, gmContent{Role: role, Parts: []gmPart{{Text: m.Content}}})
		}
	default:
		return nil, contract.ErrInvalidInput
	}
	if gc != nil {
		req.GenerationConfig = gc
	}
	return json.Marshal(&req)
}

// normalizeGeminiRole 将通用 Chat 角色映射为 Gemini 支持的集合：user|model。
// 规则：assistant→model，system→user，其余未知→user；大小写不敏感。
func normalizeGeminiRole(r string) string {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "model":
		return "model"
	case "user":
		return "user"
	case "assistant":
		return "model"
	case "system":
		return "user"
	default:
		return "user"
	}
}

func (c *Client) Invoke(ctx context.Context, b contract.Batch, p contract.Prompt) (contract.Raw, error) {
	// 从 Prompt 中抽取 JSON Schema（若存在）；默认不启用 JSON 模式，只有传入 schema 时才开启
	pp, schema := extractJSONSchemaFromPrompt(p)
	var genCfg *gmGenerationConfig
	if len(schema) > 0 {
		mime := c.respMIME
		if mime == "" {
			mime = "application/json"
		}
		genCfg = &gmGenerationConfig{ResponseMIMEType: mime, ResponseSchema: schema}
	}

	body, err := encodePrompt(pp, genCfg)
	if err != nil {
		if errors.Is(err, contract.ErrInvalidInput) {
			return contract.Raw{}, err
		}
		return contract.Raw{}, fmt.Errorf("encode: %v: %w", err, contract.ErrInvalidInput)
	}
	// 构造 URL 并安全追加 query 参数
	u, err := url.Parse(c.url)
	if err != nil {
		return contract.Raw{}, fmt.Errorf("invalid url: %v: %w", err, contract.ErrInvalidInput)
	}
	q := u.Query()
	if c.inQuery {
		q.Set("key", c.apiKey)
	}
	for k, v := range c.extraQ {
		if k == "" {
			continue
		}
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return contract.Raw{}, fmt.Errorf("new request: %v: %w", err, contract.ErrInvalidInput)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if !c.inQuery {
		req.Header.Set("x-goog-api-key", c.apiKey)
	}
	for k, v := range c.extraH {
		if k != "" {
			req.Header.Set(k, v)
		}
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
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		msg := strings.TrimSpace(string(slurp))
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode/100 == 5 {
			return contract.Raw{}, upstreamError{status: resp.StatusCode, msg: msg}
		}
		return contract.Raw{}, fmt.Errorf("gemini upstream %d: %w", resp.StatusCode, contract.ErrInvalidInput)
	}
	var gr gmResp
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&gr); err != nil {
		return contract.Raw{}, fmt.Errorf("decode: %w", contract.ErrResponseInvalid)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 || gr.Candidates[0].Content.Parts[0].Text == "" {
		return contract.Raw{}, contract.ErrResponseInvalid
	}
	return contract.Raw{Text: gr.Candidates[0].Content.Parts[0].Text}, nil
}
