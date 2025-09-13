package translate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"text/template"

	"llmspt/pkg/contract"
)

// Options 为“窗口化字幕翻译（批处理 + Chat）” PromptBuilder 的最小配置。
// - InlineSystemTemplate / SystemTemplatePath: system 提示模板（二选一，均为空时使用内置默认模板）。
type Options struct {
	InlineSystemTemplate string `json:"inline_system_template"`
	SystemTemplatePath   string `json:"system_template_path"`
	// 术语对照表（可选）：与 inline/system 一样的二选一优先级；若提供则自动拼接进 system 提示尾部。
	InlineGlossary string `json:"inline_glossary"`
	GlossaryPath   string `json:"glossary_path"`
}

// Builder: 以 Batch 构造 ChatPrompt（system+user），仅支持批处理语义。
// 运行期不做 I/O；模板在构造期解析。
type Builder struct {
	sysT *template.Template
	glos string
}

// New 创建字幕翻译 PromptBuilder（批处理 + Chat）。
func New(opts *Options) (*Builder, error) {
	o := Options{}
	if opts != nil {
		o = *opts
	}

	// 加载 system 模板（构造期 I/O）。
	src := defaultSystemTemplate
	if o.InlineSystemTemplate != "" {
		src = o.InlineSystemTemplate
	} else if o.SystemTemplatePath != "" {
		b, err := os.ReadFile(o.SystemTemplatePath)
		if err != nil {
			return nil, fmt.Errorf("system template read: %w", err)
		}
		src = string(b)
	}
	tpl, err := template.New("system").Parse(src)
	if err != nil {
		return nil, fmt.Errorf("system template parse: %w", err)
	}
	// 加载 glossary（构造期 I/O）。
	var glos string
	if o.InlineGlossary != "" {
		glos = o.InlineGlossary
	} else if o.GlossaryPath != "" {
		b, err := os.ReadFile(o.GlossaryPath)
		if err != nil {
			return nil, fmt.Errorf("glossary read: %w", err)
		}
		glos = string(b)
	}

	return &Builder{sysT: tpl, glos: glos}, nil
}

// Build: 基于 Batch 构造 ChatPrompt（system+user）。
func (b *Builder) Build(ctx context.Context, batch contract.Batch) (contract.Prompt, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(batch.Records) == 0 {
		return nil, fmt.Errorf("prompt: %w: empty batch records", contract.ErrInvalidInput)
	}
	left, target, right := splitView(batch)
	if len(target) == 0 {
		return nil, fmt.Errorf("prompt: %w: empty target window", contract.ErrInvalidInput)
	}

	// system 渲染
	var sysBuf bytes.Buffer
	if err := b.sysT.Execute(&sysBuf, nil); err != nil {
		return nil, fmt.Errorf("system render: %w", contract.ErrInvalidInput)
	}
	sys := sysBuf.String()
	if b.glos != "" {
		// 将术语对照表以 <glossary> 包裹追加至 system 尾部，遵循模板中的优先级约定
		var sb bytes.Buffer
		sb.Grow(len(sys) + len(b.glos) + 32)
		sb.WriteString(sys)
		sb.WriteString("\n\n<glossary>\n")
		sb.WriteString(b.glos)
		if !bytes.HasSuffix([]byte(b.glos), []byte("\n")) {
			sb.WriteByte('\n')
		}
		sb.WriteString("</glossary>")
		sys = sb.String()
	}

	// user 组装：窗口与批处理约束
	var uw bytes.Buffer
	uw.Grow(1024)
	uw.WriteString("### Context Window\n\n<window>\n")
	writeSegs(&uw, left)
	writeSegs(&uw, target)
	writeSegs(&uw, right)
	uw.WriteString("</window>\n")

	uw.WriteString("\nIMPORTANT OUTPUT RULES:\n")
	uw.WriteString("1) Translate ONLY segs whose ids are listed in 'targets' below.\n")
	uw.WriteString("2) Return ONLY strict JSON (no markdown, no code fences, no commentary).\n")
	uw.WriteString("3) Schema: an array of objects [{\"id\": number, \"text\": string}] in ascending id order.\n")
	uw.WriteString("targets: [")
	for i, r := range target {
		if i > 0 {
			uw.WriteByte(',')
		}
		uw.WriteString(strconv.FormatInt(int64(r.Index), 10))
	}
	uw.WriteString("]\n")

	// 输出 ChatPrompt：system + user + json_schema（用于 Gemini/OpenAI JSON 模式）
	return contract.ChatPrompt([]contract.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: uw.String()},
		{Role: "json_schema", Content: defaultTranslateJSONSchema},
	}), nil
}

// EstimateOverheadTokens: 估算与批无关的固定提示词开销（system+glossary+固定 user 规则+schema）。
// 注：不包含窗口与 targets 的动态部分；返回近似 token 数。
func (b *Builder) EstimateOverheadTokens(estimate contract.TokenEstimator) int {
	if estimate == nil {
		return 0
	}
	// system 渲染（与 Build 保持一致）
	var sysBuf bytes.Buffer
	_ = b.sysT.Execute(&sysBuf, nil)
	sys := sysBuf.String()
	if b.glos != "" {
		var sb bytes.Buffer
		sb.Grow(len(sys) + len(b.glos) + 32)
		sb.WriteString(sys)
		sb.WriteString("\n\n<glossary>\n")
		sb.WriteString(b.glos)
		if !bytes.HasSuffix([]byte(b.glos), []byte("\n")) {
			sb.WriteByte('\n')
		}
		sb.WriteString("</glossary>")
		sys = sb.String()
	}

	// user 固定部分（不包含窗口/targets 数字）
	var userFixed bytes.Buffer
	userFixed.WriteString("### Context Window\n\n<window>\n")
	userFixed.WriteString("</window>\n")
	userFixed.WriteString("\nIMPORTANT OUTPUT RULES:\n")
	userFixed.WriteString("1) Translate ONLY segs whose ids are listed in 'targets' below.\n")
	userFixed.WriteString("2) Return ONLY strict JSON (no markdown, no code fences, no commentary).\n")
	userFixed.WriteString("3) Schema: an array of objects [{\"id\": number, \"text\": string}] in ascending id order.\n")
	userFixed.WriteString("targets: []\n")

	// schema 固定部分（若 LLM 客户端忽略该消息，不会造成问题；预扣略有冗余但安全）
	schema := defaultTranslateJSONSchema

	// 汇总估算
	tokens := 0
	tokens += estimate(sys)
	tokens += estimate(userFixed.String())
	tokens += estimate(schema)
	return tokens
}

// 静态接口断言
var _ contract.PromptBuilder = (*Builder)(nil)

// splitView: 按 Batch.TargetFrom/To 切分为 left/target/right（只读）。
func splitView(b contract.Batch) (left, target, right []contract.Record) {
	l := int(b.TargetFrom)
	r := int(b.TargetTo)
	for _, rec := range b.Records {
		idx := int(rec.Index)
		if idx < l {
			left = append(left, rec)
			continue
		}
		if idx > r {
			right = append(right, rec)
			continue
		}
		target = append(target, rec)
	}
	return
}

// writeSegs: 输出 <seg id="...">\n<text>\n</seg> 形式。
func writeSegs(w *bytes.Buffer, recs []contract.Record) {
	for _, r := range recs {
		w.WriteString("<seg id=\"")
		w.WriteString(strconv.FormatInt(int64(r.Index), 10))
		w.WriteString("\">\n")
		w.WriteString(r.Text)
		w.WriteString("\n</seg>\n")
	}
}

// 默认 system 模板。
const defaultSystemTemplate = `
## Role Definition
You are a master translator tasked with translating an entire movie's subtitle content. Your goal is to provide an accurate and contextually appropriate translation while maintaining consistency in character names and understanding the meaning based on the context.

## I/O Protocol (Very Important)
- The user message will include a window container and optional glossary:
  - <window> contains multiple <seg id="..."> blocks. Preserve semantic context from the whole window.
  - Only translate the seg ids listed by the user message in "targets". Do NOT translate or rewrite other segs.
  - If a <glossary> is present, its term mappings MUST take precedence.
- When explicitly asked to return JSON (batch mode), output ONLY strict JSON according to the schema; do not include markdown/code fences.

<example>
user: <window>
<seg id="20">Context before</seg>
<seg id="21">- Hi, everyone!\n- Hello!</seg>
<seg id="22">Please be seated.</seg>
<seg id="23">Context after</seg>
</window>
Translate ONLY segs whose ids are listed in 'targets' below.
targets: [21, 22]

assistant: [{"id": 21, "text": "- 大家好！\n- 你好！"}, {"id": 22, "text": "请坐。"}]
</example>
`

// 静态接口断言
var _ contract.PromptBuilder = (*Builder)(nil)

// 针对字幕批处理的最小 JSON Schema：数组，每项含 {id:int, text:string}
const defaultTranslateJSONSchema = `{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"integer"},"text":{"type":"string"}},"required":["id","text"]}}`
