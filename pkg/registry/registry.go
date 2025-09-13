package registry

import (
	"bytes"
	"encoding/json"

	"llmspt/pkg/contract"
	linear "llmspt/plugins/assembler/linear"
	psld "llmspt/plugins/batcher/sliding"
	dsrt "llmspt/plugins/decoder/srtjson"
	gmi "llmspt/plugins/llmclient/gemini"
        mock "llmspt/plugins/llmclient/mock"
        flaky "llmspt/plugins/llmclient/flaky"
	oai "llmspt/plugins/llmclient/openai"
	ppt "llmspt/plugins/prompt/translate"
	rfs "llmspt/plugins/reader/filesystem"
	ssrt "llmspt/plugins/splitter/srt"
	wfs "llmspt/plugins/writer/filesystem"
)

// strictUnmarshal: 使用 DisallowUnknownFields 严格解码，拒绝未知字段。
func strictUnmarshal(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		// 保持零值（默认选项）
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// NewReader 工厂签名：接收原样 JSON Options。
type NewReader func(raw json.RawMessage) (contract.Reader, error)

// NewSplitter 工厂签名：接收原样 JSON Options。
type NewSplitter func(raw json.RawMessage) (contract.Splitter, error)

// NewBatcher 工厂签名：接收原样 JSON Options。
type NewBatcher func(raw json.RawMessage) (contract.Batcher, error)

// NewPromptBuilder 工厂签名：接收原样 JSON Options。
type NewPromptBuilder func(raw json.RawMessage) (contract.PromptBuilder, error)

// NewLLMClient 工厂签名：接收原样 JSON Options。
type NewLLMClient func(raw json.RawMessage) (contract.LLMClient, error)

// NewDecoder 工厂签名：接收原样 JSON Options。
type NewDecoder func(raw json.RawMessage) (contract.Decoder, error)

// NewAssembler 工厂签名：接收原样 JSON Options。
type NewAssembler func(raw json.RawMessage) (contract.Assembler, error)

// NewWriter 工厂签名：接收原样 JSON Options。
type NewWriter func(raw json.RawMessage) (contract.Writer, error)

// Reader 工厂注册表（显式、零反射）。
var Reader = map[string]NewReader{
	// fs: 文件系统/STDIN Reader
	"fs": func(raw json.RawMessage) (contract.Reader, error) {
		var opts rfs.Options
		if err := strictUnmarshal(raw, &opts); err != nil {
			return nil, err
		}
		return rfs.New(&opts), nil
	},
}

// Splitter 工厂注册表。
var Splitter = map[string]NewSplitter{
	// srt: SRT 拆分器
	"srt": func(raw json.RawMessage) (contract.Splitter, error) {
		var opts ssrt.Options
		if err := strictUnmarshal(raw, &opts); err != nil {
			return nil, err
		}
		return ssrt.New(&opts), nil
	},
}

// Batcher 工厂注册表。
var Batcher = map[string]NewBatcher{
	// sliding: 固定上下文滑动窗口批处理
	"sliding": func(raw json.RawMessage) (contract.Batcher, error) {
		var opts psld.Options
		if err := strictUnmarshal(raw, &opts); err != nil {
			return nil, err
		}
		return psld.New(&opts), nil
	},
}

// PromptBuilder 工厂注册表。
var PromptBuilder = map[string]NewPromptBuilder{
	// translate: 窗口化字幕 PromptBuilder（批处理 + Chat）
	"translate": func(raw json.RawMessage) (contract.PromptBuilder, error) {
		var opts ppt.Options
		if err := strictUnmarshal(raw, &opts); err != nil {
			return nil, err
		}
		return ppt.New(&opts)
	},
}

// LLMClient 工厂注册表。
var LLMClient = map[string]NewLLMClient{
        "openai": func(raw json.RawMessage) (contract.LLMClient, error) { return oai.New(raw) },
        "gemini": func(raw json.RawMessage) (contract.LLMClient, error) { return gmi.New(raw) },
        "mock":   func(raw json.RawMessage) (contract.LLMClient, error) { return mock.New(raw) },
        "flaky":  func(raw json.RawMessage) (contract.LLMClient, error) { return flaky.New(raw) },
}

// Decoder 工厂注册表。
var Decoder = map[string]NewDecoder{
	// srt: 翻译（逐条 JSON 数组）解码器（每条 [{id:int,text:string,meta?:object}]）
	"srt": func(raw json.RawMessage) (contract.Decoder, error) { return dsrt.New(raw) },
}

// Assembler 工厂注册表。
var Assembler = map[string]NewAssembler{
	// srt: 使用 Meta["seq"], Meta["time"] 还原 SRT 头两行并拼接 Output
	"linear": func(raw json.RawMessage) (contract.Assembler, error) { return linear.New(raw) },
}

// Writer 工厂注册表。
var Writer = map[string]NewWriter{
	// fs: 文件系统 Writer（覆盖写/原子替换可配置）
	"fs": func(raw json.RawMessage) (contract.Writer, error) {
		var opts wfs.Options
		if err := strictUnmarshal(raw, &opts); err != nil {
			return nil, err
		}
		return wfs.New(&opts)
	},
}
