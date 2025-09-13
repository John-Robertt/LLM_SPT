package config

import "encoding/json"

// DefaultTemplateConfig 返回一个“可运行”的默认配置模板：
// - 使用 mock LLM 与合理限额（本地/离线调试友好）；
// - 默认输入为 STDIN（"-"），Writer 输出到 ./out 目录；
// - 组件名采用仓库内置实现；
// - 选项给出安全中性默认值。
func DefaultTemplateConfig() Config {
	d := Defaults()
    cfg := Config{
        Inputs:      []string{"-"},
		Concurrency: d.Concurrency,
		MaxTokens:   2048,
		MaxRetries:  2,
		Logging:     Logging{Level: "info"},
		Components:  d.Components,
		LLM:         "mock",
		Provider: map[string]Provider{
			"mock": {
				Client: "mock",
				// 包含所有 mock 选项键（可为空）
				Options: json.RawMessage(`{"prefix":"","api_key":"","response_mode":""}`),
				Limits:  Limits{RPM: 60, TPM: 10000, MaxTokensPerReq: 4096},
			},
            "openai": {
                Client: "openai",
                // 覆盖全部 OpenAI 选项键，值可为空/默认
                Options: json.RawMessage(`{
  "base_url": "",
  "model": "", 
  "api_key_env": "",
  "api_key": "",
  "timeout_seconds": 60,
  "temperature": null,
  "endpoint_path": "",
  "disable_default_auth": false,
  "extra_headers": {}
}`),
                Limits: Limits{RPM: 0, TPM: 0, MaxTokensPerReq: 0},
            },
            "gemini": {
                Client: "gemini",
                // 覆盖全部 Gemini 选项键，值可为空/默认
                Options: json.RawMessage(`{
  "base_url": "",
  "model": "",
  "api_key_env": "",
  "api_key": "",
  "endpoint_path": "",
  "timeout_seconds": 60,
  "api_key_in_query": true,
  "extra_headers": {},
  "extra_query": {},
  "response_mime_type": ""
}`),
                Limits: Limits{RPM: 0, TPM: 0, MaxTokensPerReq: 0},
            },
        },
    }
	// Options：包含所有键（值可为空/默认），确保键存在。
	cfg.Options.Reader = json.RawMessage(`{
  "buf_size": 65536,
  "exclude_dir_names": [".git", "node_modules", "vendor"]
}`)
	cfg.Options.Splitter = json.RawMessage(`{
  "max_fragment_bytes": 0,
  "allow_exts": [".srt"]
}`)
    cfg.Options.Batcher = json.RawMessage(`{
  "context_radius": 1,
  "bytes_per_token": 4,
  "extra_bytes_per_record": 80
}`)
	cfg.Options.Writer = json.RawMessage(`{
  "output_dir": "out",
  "atomic": true,
  "flat": true,
  "perm_file": 0,
  "perm_dir": 0,
  "buf_size": 65536
}`)
	cfg.Options.PromptBuilder = json.RawMessage(`{
  "inline_system_template": "",
  "system_template_path": "",
  "inline_glossary": "",
  "glossary_path": ""
}`)
	// decoder.srt 当前无配置项，保持空对象
	cfg.Options.Decoder = json.RawMessage(`{}`)
	// srt 装配器无配置项，保持空对象
	cfg.Options.Assembler = json.RawMessage(`{}`)
	return cfg
}
