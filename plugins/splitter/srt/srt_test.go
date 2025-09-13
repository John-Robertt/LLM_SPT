package srt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const sample = "1\n00:00:01,000 --> 00:00:02,000\nhello\n\n2\n00:00:02,000 --> 00:00:03,000\nworld\n\n"

// TestSplitSuccess 测试合法 SRT 分割
func TestSplitSuccess(t *testing.T) {
	s := New(nil)
	recs, err := s.Split(context.Background(), "a.srt", strings.NewReader(sample))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(recs) != 2 || recs[1].Index != 1 {
		t.Fatalf("unexpected recs %+v", recs)
	}
	if recs[0].Meta["seq"] != "1" || recs[1].Meta["seq"] != "2" {
		t.Fatalf("meta missing")
	}
}

// TestSplitTooLarge 超出 MaxFragmentBytes
func TestSplitTooLarge(t *testing.T) {
	s := New(&Options{MaxFragmentBytes: 3})
	_, err := s.Split(context.Background(), "a.srt", strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\nabcdef\n\n"))
	if err == nil {
		t.Fatalf("expect size error")
	}
}

// TestSplitExtFilter 扩展名过滤
func TestSplitExtFilter(t *testing.T) {
	s := New(nil) // 默认只允许 .srt
	recs, err := s.Split(context.Background(), "a.txt", strings.NewReader(sample))
	if err != nil || recs != nil {
		t.Fatalf("non-srt should be ignored without error")
	}
}

// TestSplitFormatError 格式错误
func TestSplitFormatError(t *testing.T) {
	s := New(nil)
	_, err := s.Split(context.Background(), "a.srt", strings.NewReader("bad"))
	if err == nil {
		t.Fatalf("expect format error")
	}
}

// TestSplitInvalidTimeLine 时间轴行非法
func TestSplitInvalidTimeLine(t *testing.T) {
	s := New(nil)
	_, err := s.Split(context.Background(), "a.srt", strings.NewReader("1\nBAD\n"))
	if err == nil {
		t.Fatalf("expect time line error")
	}
}

// TestSplitInvalidUTF8 文本包含非法 UTF-8
func TestSplitInvalidUTF8(t *testing.T) {
	s := New(nil)
	data := "1\n00:00:00,000 --> 00:00:01,000\n" + string([]byte{0xff}) + "\n\n"
	_, err := s.Split(context.Background(), "a.srt", strings.NewReader(data))
	if err == nil {
		t.Fatalf("expect utf8 error")
	}
}

// TestSplitAllowExtsCustom 自定义扩展名
func TestSplitAllowExtsCustom(t *testing.T) {
	s := New(&Options{AllowExts: []string{".txt"}})
	recs, err := s.Split(context.Background(), "a.TXT", strings.NewReader(sample))
	if err != nil || recs == nil {
		t.Fatalf("custom ext failed %v", err)
	}
}

// TestSplitAllowExtsAll 空列表允许所有扩展
func TestSplitAllowExtsAll(t *testing.T) {
	s := New(&Options{AllowExts: []string{}})
	recs, err := s.Split(context.Background(), "a.md", strings.NewReader(sample))
	if err != nil || len(recs) != 2 {
		t.Fatalf("allow all ext failed %v %d", err, len(recs))
	}
}

// TestSplitCtxCancel 上下文取消
func TestSplitCtxCancel(t *testing.T) {
	s := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Split(ctx, "a.srt", strings.NewReader(sample))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expect ctx cancel, got %v", err)
	}
}
