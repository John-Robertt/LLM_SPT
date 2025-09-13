package sliding

import (
	"context"
	"strings"
	"testing"

	"llmspt/pkg/contract"
)

// TestMakeSuccess 测试滑动窗口正常批处理
func TestMakeSuccess(t *testing.T) {
	b := New(&Options{ContextRadius: 1, BytesPerToken: 1})
	recs := []contract.Record{
		{Index: 0, FileID: "f", Text: "a"},
		{Index: 1, FileID: "f", Text: "b"},
		{Index: 2, FileID: "f", Text: "c"},
	}
	batches, err := b.Make(context.Background(), recs, contract.BatchLimit{MaxTokens: 10})
	if err != nil {
		t.Fatalf("make: %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("expect 1 batch, got %d", len(batches))
	}
	if batches[0].TargetFrom != 0 || batches[0].TargetTo != 2 {
		t.Fatalf("unexpected target range %+v", batches[0])
	}
}

// TestMakeTargetTooLarge 测试单目标过大放不下
func TestMakeTargetTooLarge(t *testing.T) {
	b := New(&Options{ContextRadius: 1, BytesPerToken: 1})
	recs := []contract.Record{
		{Index: 0, FileID: "f", Text: "aaa"},
		{Index: 1, FileID: "f", Text: "bbb"},
	}
	_, err := b.Make(context.Background(), recs, contract.BatchLimit{MaxTokens: 4})
	if err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Fatalf("expect single target too large error, got %v", err)
	}
}

// TestMakeIndexError 测试索引不连续
func TestMakeIndexError(t *testing.T) {
	b := New(nil)
	recs := []contract.Record{
		{Index: 0, FileID: "f", Text: "a"},
		{Index: 2, FileID: "f", Text: "b"},
	}
	_, err := b.Make(context.Background(), recs, contract.BatchLimit{MaxTokens: 10})
	if err == nil {
		t.Fatalf("expect error for non-contiguous index")
	}
}

// TestMakeBadLimit 测试无效预算
func TestMakeBadLimit(t *testing.T) {
	b := New(nil)
	_, err := b.Make(context.Background(), nil, contract.BatchLimit{MaxTokens: 0})
	if err == nil {
		t.Fatalf("expect error for bad limit")
	}
}

// TestMakeFileIDMismatch 测试 FileID 不一致
func TestMakeFileIDMismatch(t *testing.T) {
	b := New(nil)
	recs := []contract.Record{{Index: 0, FileID: "a", Text: "x"}, {Index: 1, FileID: "b", Text: "y"}}
	_, err := b.Make(context.Background(), recs, contract.BatchLimit{MaxTokens: 10})
	if err == nil {
		t.Fatalf("expect error for fileid mismatch")
	}
}

// TestMakeCtxCancel 测试上下文取消
func TestMakeCtxCancel(t *testing.T) {
	b := New(nil)
	recs := []contract.Record{{Index: 0, FileID: "f", Text: "a"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Make(ctx, recs, contract.BatchLimit{MaxTokens: 10})
	if err == nil {
		t.Fatalf("expect ctx error")
	}
}

// TestEstimateTokens 覆盖估算逻辑
func TestEstimateTokens(t *testing.T) {
	b := &Batcher{bytesPerToken: 2}
	if b.estimateTokens("abcd") != 2 {
		t.Fatalf("expect 2 tokens")
	}
	b.bytesPerToken = 0
	if b.estimateTokens("") != 0 || b.estimateTokens("aa") != 1 {
		t.Fatalf("default estimate failed")
	}
}

// TestSum 覆盖求和边界
func TestSum(t *testing.T) {
	pref := []int{0, 1, 3, 6}
	if sum(pref, 2, 1) != 0 {
		t.Fatalf("a>b should 0")
	}
	if sum(pref, -1, 1) != 3 {
		t.Fatalf("negative a")
	}
	if sum(pref, 0, 10) != 6 {
		t.Fatalf("b overflow")
	}
}
