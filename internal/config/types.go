package config

import (
	"encoding/json"
)

// Config: 运行期只读配置（一次解析，运行期不变）。
// JSON 使用 snake_case；未知字段在解析期失败。
type Config struct {
	Inputs      []string `json:"inputs"`
	Concurrency int      `json:"concurrency"`
	MaxTokens   int      `json:"max_tokens"`
	// MaxRetries: LLM 阶段最大重试次数（>=0）。0 表示不重试。
	MaxRetries int     `json:"max_retries"`
	Logging    Logging `json:"logging"`

	// 组件名选择（空则使用默认名）。
	Components Components `json:"components"`

	// LLM Provider 选择与定义。
	LLM      string              `json:"llm"`
	Provider map[string]Provider `json:"provider"`

	// 各组件 Options 子树，原样 JSON 传入工厂。
	Options Options `json:"options"`
}

// Logging: 仅保留日志等级可配置；输出路径与轮转策略为固定默认。
type Logging struct {
	Level string `json:"level"`
}

// Components: 组件名选择（注册表中的实现名）。
type Components struct {
	Reader        string `json:"reader"`
	Splitter      string `json:"splitter"`
	Batcher       string `json:"batcher"`
	Writer        string `json:"writer"`
	PromptBuilder string `json:"prompt_builder"`
	Decoder       string `json:"decoder"`
	Assembler     string `json:"assembler"`
}

// Options: 各组件的原样 JSON Options。
type Options struct {
	Reader        json.RawMessage `json:"reader"`
	Splitter      json.RawMessage `json:"splitter"`
	Batcher       json.RawMessage `json:"batcher"`
	Writer        json.RawMessage `json:"writer"`
	PromptBuilder json.RawMessage `json:"prompt_builder"`
	Decoder       json.RawMessage `json:"decoder"`
	Assembler     json.RawMessage `json:"assembler"`
}

// Provider: 命名 provider 定义（client 实现 + options + 限额）。
type Provider struct {
	Client  string          `json:"client"`
	Options json.RawMessage `json:"options"`
	Limits  Limits          `json:"limits"`
}

// Limits: 限流配置（仅承载；执行位于 rate.Gate）。
type Limits struct {
	RPM             int `json:"rpm"`
	TPM             int `json:"tpm"`
	MaxTokensPerReq int `json:"max_tokens_per_req"`
}
