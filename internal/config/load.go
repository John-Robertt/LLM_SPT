package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Defaults 返回带有安全默认值的 Config 雏形。
// 注意：LLM 不设默认（必须由 JSON/ENV/CLI 提供）。
func Defaults() Config {
	return Config{
		Concurrency: 1,
		MaxRetries:  0,
		Components: Components{
			Reader:        "fs",
			Splitter:      "srt",
			Batcher:       "sliding",
			Writer:        "fs",
			PromptBuilder: "translate",
			Decoder:       "srt",
			Assembler:     "linear",
		},
	}
}

// LoadJSON 从文件路径或原始 JSON 解析 Config（严格拒绝未知字段）。
func LoadJSON(path string, raw []byte) (Config, error) {
	var cfg Config
	var r io.Reader
	switch {
	case len(raw) > 0:
		r = bytes.NewReader(raw)
	case path != "":
		f, err := os.Open(path)
		if err != nil {
			return cfg, err
		}
		defer f.Close()
		r = f
	default:
		return cfg, errors.New("no config source provided")
	}
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Merge 按优先级合并（后者覆盖前者）。
// 仅标量/字符串/原样 JSON 为“替换”；不做深度合并。
func Merge(base, over Config) Config {
    out := base
    // 顶层
    if len(over.Inputs) > 0 {
        out.Inputs = cloneStrings(over.Inputs)
    }
    if over.Concurrency != 0 {
        out.Concurrency = over.Concurrency
    }
    if over.MaxTokens != 0 {
        out.MaxTokens = over.MaxTokens
    }
    // 特殊：MaxRetries 的 0 具有语义（禁用重试），需要显式可覆盖。
    // 约定：当 over.MaxRetries >= 0 时认为“存在”，否则（例如 -1）视为未覆盖。
    if over.MaxRetries >= 0 {
        out.MaxRetries = over.MaxRetries
    }
	// Logging（仅 level）
	if strings.TrimSpace(over.Logging.Level) != "" {
		out.Logging.Level = strings.TrimSpace(over.Logging.Level)
	}

	// 组件名（空不覆盖）
	if over.Components.Reader != "" {
		out.Components.Reader = over.Components.Reader
	}
	if over.Components.Splitter != "" {
		out.Components.Splitter = over.Components.Splitter
	}
	if over.Components.Batcher != "" {
		out.Components.Batcher = over.Components.Batcher
	}
	if over.Components.Writer != "" {
		out.Components.Writer = over.Components.Writer
	}
	if over.Components.PromptBuilder != "" {
		out.Components.PromptBuilder = over.Components.PromptBuilder
	}
	if over.Components.Decoder != "" {
		out.Components.Decoder = over.Components.Decoder
	}
	if over.Components.Assembler != "" {
		out.Components.Assembler = over.Components.Assembler
	}

	// Provider（完整替换对应键）
	if len(over.Provider) > 0 {
		if out.Provider == nil {
			out.Provider = make(map[string]Provider, len(over.Provider))
		}
		for k, v := range over.Provider {
			out.Provider[k] = v
		}
	}

	// Options（完整替换对应键）
	if len(over.Options.Reader) > 0 {
		out.Options.Reader = cloneRaw(over.Options.Reader)
	}
	if len(over.Options.Splitter) > 0 {
		out.Options.Splitter = cloneRaw(over.Options.Splitter)
	}
	if len(over.Options.Batcher) > 0 {
		out.Options.Batcher = cloneRaw(over.Options.Batcher)
	}
	if len(over.Options.Writer) > 0 {
		out.Options.Writer = cloneRaw(over.Options.Writer)
	}
	if len(over.Options.PromptBuilder) > 0 {
		out.Options.PromptBuilder = cloneRaw(over.Options.PromptBuilder)
	}
	if len(over.Options.Decoder) > 0 {
		out.Options.Decoder = cloneRaw(over.Options.Decoder)
	}
	if len(over.Options.Assembler) > 0 {
		out.Options.Assembler = cloneRaw(over.Options.Assembler)
	}

	// LLM 名称
	if strings.TrimSpace(over.LLM) != "" {
		out.LLM = strings.TrimSpace(over.LLM)
	}
	return out
}

// EnvOverlay 从环境变量构建一个 Config 覆盖（仅解析有限键集合）。
// 规则：前缀 LLM_SPT_；未知但匹配本集合之外的键忽略（保持 5.1 边界最小化）。
// 支持：INPUTS, CONCURRENCY, MAX_TOKENS, LLM, COMPONENTS_*
// 以及 PROVIDER__<name>__CLIENT / PROVIDER__<name>__LIMITS_{RPM,TPM,MAX_TOKENS_PER_REQ} / PROVIDER__<name>__OPTIONS_JSON
func EnvOverlay(environ []string) (Config, error) {
    var over Config
    // 默认：-1 表示未设置，以便 Merge 能区分“未覆盖”和“显式设置为 0”。
    over.MaxRetries = -1
    // provider 聚合
    prov := map[string]Provider{}
    for _, kv := range environ {
        if !strings.HasPrefix(kv, "LLM_SPT_") {
            continue
        }
        eq := strings.IndexByte(kv, '=')
        if eq <= len("LLM_SPT_") {
            continue
        }
        key := kv[:eq]
        val := kv[eq+1:]
        nk := strings.TrimPrefix(key, "LLM_SPT_")
        switch nk {
		case "INPUTS":
			if val != "" {
				over.Inputs = splitComma(val)
			}
		case "CONCURRENCY":
			if v, err := atoi(val); err == nil {
				over.Concurrency = v
			}
		case "MAX_TOKENS":
			if v, err := atoi(val); err == nil {
				over.MaxTokens = v
			}
        case "MAX_RETRIES":
            if v, err := atoi(val); err == nil {
                over.MaxRetries = v
            }
		case "LLM":
			over.LLM = strings.TrimSpace(val)
		case "COMPONENTS_READER":
			over.Components.Reader = strings.TrimSpace(val)
		case "COMPONENTS_SPLITTER":
			over.Components.Splitter = strings.TrimSpace(val)
		case "COMPONENTS_BATCHER":
			over.Components.Batcher = strings.TrimSpace(val)
		case "COMPONENTS_WRITER":
			over.Components.Writer = strings.TrimSpace(val)
		case "COMPONENTS_PROMPT_BUILDER":
			over.Components.PromptBuilder = strings.TrimSpace(val)
		case "COMPONENTS_DECODER":
			over.Components.Decoder = strings.TrimSpace(val)
		case "COMPONENTS_ASSEMBLER":
			over.Components.Assembler = strings.TrimSpace(val)
        default:
            // provider.* 路径：PROVIDER__name__FOO
            if strings.HasPrefix(nk, "PROVIDER__") {
                parts := strings.Split(nk, "__")
                if len(parts) >= 3 {
                    name := strings.TrimSpace(parts[1])
                    field := strings.Join(parts[2:], "__")
                    p := prov[name]
                    changed := false
                    switch field {
                    case "CLIENT":
                        if tv := strings.TrimSpace(val); tv != "" {
                            p.Client = tv
                            changed = true
                        }
                    case "LIMITS_RPM":
                        if v, err := atoi(val); err == nil {
                            p.Limits.RPM = v
                            changed = true
                        }
                    case "LIMITS_TPM":
                        if v, err := atoi(val); err == nil {
                            p.Limits.TPM = v
                            changed = true
                        }
                    case "LIMITS_MAX_TOKENS_PER_REQ":
                        if v, err := atoi(val); err == nil {
                            p.Limits.MaxTokensPerReq = v
                            changed = true
                        }
                    case "OPTIONS_JSON":
                        // 原样 JSON；空值视为未设置，避免清空现有配置
                        if strings.TrimSpace(val) != "" {
                            p.Options = json.RawMessage(val)
                            changed = true
                        }
                    default:
                        // 非本 5.1 集合的键忽略（例如日志/观测等章节的 ENV）。
                    }
                    // 仅在发生有效变更时记录该 provider；避免空值覆盖 config.json
                    if changed {
                        prov[name] = p
                    }
                }
            }
        }
    }
    if len(prov) > 0 {
        over.Provider = prov
    }
    return over, nil
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneRaw(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func atoi(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
