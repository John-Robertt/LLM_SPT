package srtjson

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"llmspt/pkg/contract"
)

// TestDecodeInvalidJSON 测试非法 JSON
func TestDecodeInvalidJSON(t *testing.T) {
	d, _ := New(nil)
	_, err := d.Decode(context.Background(), contract.Target{FileID: "f", From: 0, To: 0}, contract.Raw{Text: "not"})
	if err == nil || !errors.Is(err, contract.ErrResponseInvalid) {
		t.Fatalf("expect ErrResponseInvalid, got %v", err)
	}
}

// TestDecodeSuccess 测试正常解码
func TestDecodeSuccess(t *testing.T) {
	d, _ := New(nil)
	src := `[{"id":1,"text":"hi","meta":{"seq":"1","time":"0-->1"}},{"id":2,"text":"bye"}]`
	spans, err := d.Decode(context.Background(), contract.Target{FileID: "f", From: 1, To: 2}, contract.Raw{Text: src})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("expect 2 spans")
	}
	if spans[0].Meta["seq"] != "1" || spans[0].FileID != "f" {
		t.Fatalf("meta not carried")
	}
    // 允许 decoder 注入 dst_text 辅助对照；但不应凭空构造 seq/time
    if spans[1].Meta != nil && (spans[1].Meta["seq"] != "" || spans[1].Meta["time"] != "") {
        t.Fatalf("unexpected seq/time in second span meta")
    }
	if spans[0].Output == "" || spans[1].Output == "" {
		t.Fatalf("expect non-empty outputs")
	}
}

// TestDecodeWithMeta 回填 meta
func TestDecodeWithMeta(t *testing.T) {
	dd, _ := New(nil)
	src := `[{"id":5,"text":"x"}]`
	idx := contract.IndexMetaMap{5: {"seq": "5", "time": "0-->1"}}
	spans, err := dd.(*decoder).DecodeWithMeta(context.Background(), contract.Target{FileID: "f", From: 5, To: 5}, contract.Raw{Text: src}, idx)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if spans[0].Meta["seq"] != "5" {
		b, _ := json.Marshal(spans[0])
		t.Fatalf("meta not filled: %s", b)
	}
}

// 当返回 text 为空时，视为协议失败（ErrResponseInvalid）
func TestDecodeWithMetaEmptyFails(t *testing.T) {
    dd, _ := New(nil)
    src := `[{"id":7,"text":"  ","meta":{}}]`
    idx := contract.IndexMetaMap{7: {"seq": "7", "time": "00:00:01,000 --> 00:00:02,000", "_src_text": "原文"}}
    _, err := dd.(*decoder).DecodeWithMeta(context.Background(), contract.Target{FileID: "f", From: 7, To: 7}, contract.Raw{Text: src}, idx)
    if err == nil || !errors.Is(err, contract.ErrResponseInvalid) {
        t.Fatalf("expect ErrResponseInvalid, got %v", err)
    }
}

// Decode 路径空文本也失败
func TestDecodeEmptyFails(t *testing.T) {
    d, _ := New(nil)
    src := `[{"id":1,"text":"   "}]`
    _, err := d.Decode(context.Background(), contract.Target{FileID: "f", From: 1, To: 1}, contract.Raw{Text: src})
    if err == nil || !errors.Is(err, contract.ErrResponseInvalid) {
        t.Fatalf("expect ErrResponseInvalid, got %v", err)
    }
}

// TestNewWithRaw 非空选项解析
func TestNewWithRaw(t *testing.T) {
	if _, err := New(json.RawMessage(`{"dummy":1}`)); err != nil {
		t.Fatalf("new: %v", err)
	}
}

// TestDecodeCtxCancel 上下文取消
func TestDecodeCtxCancel(t *testing.T) {
	d, _ := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.Decode(ctx, contract.Target{FileID: "f", From: 0, To: 0}, contract.Raw{Text: "[]"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expect ctx cancel, got %v", err)
	}
}
