package registry

import (
    "encoding/json"
    "errors"
    "fmt"
    "testing"

    "llmspt/pkg/contract"
)

// TestStrictUnmarshal 验证严格解码逻辑。
func TestStrictUnmarshal(t *testing.T) {
    type opt struct{ A int `json:"a"` }
    var o opt
    if err := strictUnmarshal(nil, &o); err != nil || o.A != 0 {
        t.Fatalf("nil 输入失败: %v", err)
    }
    if err := strictUnmarshal(json.RawMessage(`{"a":1}`), &o); err != nil || o.A != 1 {
        t.Fatalf("合法 JSON 解析失败: %v", err)
    }
    if err := strictUnmarshal(json.RawMessage(`{"a":1,"b":2}`), &o); err == nil {
        t.Fatalf("未知字段应报错")
    }
}

// TestFactories 遍历注册表入口。
func TestFactories(t *testing.T) {
    t.Run("reader", func(t *testing.T) {
        if _, err := Reader["fs"](json.RawMessage(`{}`)); err != nil {
            t.Fatalf("reader: %v", err)
        }
        if _, err := Reader["fs"](json.RawMessage(`{"x":1}`)); err == nil {
            t.Fatalf("reader 未对未知字段报错")
        }
    })
    t.Run("splitter", func(t *testing.T) {
        if _, err := Splitter["srt"](json.RawMessage(`{}`)); err != nil {
            t.Fatalf("splitter: %v", err)
        }
        if _, err := Splitter["srt"](json.RawMessage(`{"x":1}`)); err == nil {
            t.Fatalf("splitter 未对未知字段报错")
        }
    })
    t.Run("batcher", func(t *testing.T) {
        if _, err := Batcher["sliding"](json.RawMessage(`{}`)); err != nil {
            t.Fatalf("batcher: %v", err)
        }
        if _, err := Batcher["sliding"](json.RawMessage(`{"x":1}`)); err == nil {
            t.Fatalf("batcher 未对未知字段报错")
        }
    })
    t.Run("prompt", func(t *testing.T) {
        if _, err := PromptBuilder["translate"](json.RawMessage(`{}`)); err != nil {
            t.Fatalf("prompt: %v", err)
        }
        if _, err := PromptBuilder["translate"](json.RawMessage(`{"x":1}`)); err == nil {
            t.Fatalf("prompt 未对未知字段报错")
        }
    })
    t.Run("decoder", func(t *testing.T) {
        if _, err := Decoder["srt"](json.RawMessage(`{}`)); err != nil {
            t.Fatalf("decoder: %v", err)
        }
    })
    t.Run("assembler", func(t *testing.T) {
        if _, err := Assembler["linear"](json.RawMessage(`{}`)); err != nil {
            t.Fatalf("assembler: %v", err)
        }
    })
    t.Run("writer", func(t *testing.T) {
        tmp := t.TempDir()
        raw := json.RawMessage([]byte(fmt.Sprintf(`{"output_dir":%q}`, tmp)))
        if _, err := Writer["fs"](raw); err != nil {
            t.Fatalf("writer: %v", err)
        }
        bad := json.RawMessage([]byte(fmt.Sprintf(`{"output_dir":%q,"x":1}`, tmp)))
        if _, err := Writer["fs"](bad); err == nil {
            t.Fatalf("writer 未对未知字段报错")
        }
    })
    t.Run("llm-mock", func(t *testing.T) {
        if _, err := LLMClient["mock"](json.RawMessage(`{}`)); err != nil {
            t.Fatalf("mock: %v", err)
        }
    })
    t.Run("llm-openai", func(t *testing.T) {
        if _, err := LLMClient["openai"](json.RawMessage(`{}`)); !errors.Is(err, contract.ErrInvalidInput) {
            t.Fatalf("openai 未按预期报错: %v", err)
        }
    })
    t.Run("llm-gemini", func(t *testing.T) {
        if _, err := LLMClient["gemini"](json.RawMessage(`{}`)); !errors.Is(err, contract.ErrInvalidInput) {
            t.Fatalf("gemini 未按预期报错: %v", err)
        }
    })
}

