package contract

import "context"

// Prompt: 不透明载荷，由具体 PromptBuilder/LLMClient 配对解释。
type Prompt any

// Message: 最小会话消息形状（可用于 ChatPrompt）。
type Message struct {
	Role    string
	Content string
}

// TextPrompt: 文本型提示词载荷。
type TextPrompt string

// ChatPrompt: 会话型提示词载荷（最小集合）。
type ChatPrompt []Message

// PromptBuilder: 基于 Batch 构造确定性的 Prompt。
// 约束：
//   - 纯计算，不做 I/O；
//   - 不隐式修改业务内容；
//   - 失败快速返回错误。
type PromptBuilder interface {
	Build(ctx context.Context, b Batch) (Prompt, error)
	// EstimateOverheadTokens: 估算“与批无关的固定提示词开销”的近似 token 数。
	// 仅包含固定部分（如 system/glossary/固定规则/schema），不得包含窗口文本或 targets 等动态内容。
	EstimateOverheadTokens(estimate TokenEstimator) int
}

// TokenEstimator: 文本→token 的近似估算函数。
// 典型实现：ceil(len(utf8_bytes)/BytesPerToken)。
type TokenEstimator func(s string) int
