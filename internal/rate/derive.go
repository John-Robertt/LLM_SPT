package rate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
)

// DeriveKeyFromProviderOptions 从 LLM 客户端标识与其原样 Options JSON 中提取 API Key，
// 并返回按 client+sha256(key) 构造的限流分组键。找不到 key 时返回错误。
// 仅解析常见键名："api_key" 与 "api_key_env"；mock 客户端若未提供 api_key，则使用内置 "MOCK_DEBUG_KEY"。
func DeriveKeyFromProviderOptions(client string, raw json.RawMessage) (LimitKey, error) {
	// 为避免跨层依赖 plugins/* 的具体类型，这里按通用 JSON 键解析。
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)

	pick := func(m map[string]any, key string) string {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	key := ""
	switch client {
	case "openai", "gemini":
		key = pick(obj, "api_key")
		if key == "" {
			if env := pick(obj, "api_key_env"); env != "" {
				key = os.Getenv(env)
			}
		}
	case "mock":
		key = pick(obj, "api_key")
		if key == "" {
			key = "MOCK_DEBUG_KEY"
		}
	default:
		// 尝试通用键解析
		key = pick(obj, "api_key")
		if key == "" {
			if env := pick(obj, "api_key_env"); env != "" {
				key = os.Getenv(env)
			}
		}
	}

	if key == "" {
		return "", fmt.Errorf("rate: missing api key for client %s", client)
	}
	sum := sha256.Sum256([]byte(key))
	return LimitKey(fmt.Sprintf("%s:%x", client, sum[:])), nil
}
