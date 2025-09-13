package mock

import (
    "context"
    "encoding/json"
    "testing"

    "llmspt/pkg/contract"
)

// TestTranslateJSONPerRecord 模式测试
func TestTranslateJSONPerRecord(t *testing.T) {
	c, _ := New(json.RawMessage(`{"response_mode":"translate_json_per_record","prefix":"X"}`))
	batch := contract.Batch{FileID: "f", TargetFrom: 0, TargetTo: 1, Records: []contract.Record{{Index: 0, Text: "a"}, {Index: 1, Text: "b", Meta: contract.Meta{"k": "v"}}}}
	raw, err := c.Invoke(context.Background(), batch, contract.TextPrompt("hi"))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var arr []struct {
		ID   int64             `json:"id"`
		Text string            `json:"text"`
		Meta map[string]string `json:"meta"`
	}
	if err := json.Unmarshal([]byte(raw.Text), &arr); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(arr) != 2 || arr[0].ID != 0 || arr[1].Meta["k"] != "v" {
		t.Fatalf("unexpected arr %#v", arr)
	}
}

// TestLineMap 模式
func TestLineMap(t *testing.T) {
	c, _ := New(json.RawMessage(`{"response_mode":"line_map"}`))
	batch := contract.Batch{TargetFrom: 0, TargetTo: 1, Records: []contract.Record{{Index: 0, Text: "x"}, {Index: 1, Text: "y"}}}
	raw, err := c.Invoke(context.Background(), batch, contract.TextPrompt(""))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if raw.Text != "x\ny" {
		t.Fatalf("unexpected text %q", raw.Text)
	}
}

// TestTranslateJSONSpan 模式
func TestTranslateJSONSpan(t *testing.T) {
	c, _ := New(json.RawMessage(`{"response_mode":"translate_json_span"}`))
	batch := contract.Batch{TargetFrom: 0, TargetTo: 1, Records: []contract.Record{{Index: 0, Text: "x"}, {Index: 1, Text: "y"}}}
	raw, err := c.Invoke(context.Background(), batch, contract.TextPrompt(""))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var obj struct {
		From int64
		To   int64
		Text string
	}
	json.Unmarshal([]byte(raw.Text), &obj)
	if obj.From != 0 || obj.To != 1 || obj.Text != "x\ny" {
		t.Fatalf("unexpected obj %#v", obj)
	}
}

// TestDefaultPrompt 默认回显
func TestDefaultPrompt(t *testing.T) {
    // 默认模式应为 translate_json_per_record，返回严格 JSON 数组
    c, _ := New(nil)
    batch := contract.Batch{FileID: "f", TargetFrom: 0, TargetTo: 1,
        Records: []contract.Record{{Index: 0, Text: "a"}, {Index: 1, Text: "b"}},
    }
    raw, err := c.Invoke(context.Background(), batch, contract.TextPrompt("abc"))
    if err != nil {
        t.Fatalf("invoke: %v", err)
    }
    var arr []struct {
        ID   int64  `json:"id"`
        Text string `json:"text"`
    }
    if err := json.Unmarshal([]byte(raw.Text), &arr); err != nil {
        t.Fatalf("default should be json per-record: %v; text=%q", err, raw.Text)
    }
    if len(arr) != 2 || arr[0].ID != 0 || arr[1].ID != 1 {
        t.Fatalf("unexpected default items: %#v", arr)
    }
}
