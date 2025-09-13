package linear

import (
	"context"
	"io"
	"testing"

	"llmspt/pkg/contract"
)

// TestAssembleSuccess 测试正常线性拼接
func TestAssembleSuccess(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	spans := []contract.SpanResult{
		{FileID: "f", From: 0, To: 0, Output: "hello"},
		{FileID: "f", From: 1, To: 1, Output: "world"},
	}
	r, err := a.Assemble(context.Background(), "f", spans)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	b, _ := io.ReadAll(r)
	if string(b) != "helloworld" {
		t.Fatalf("unexpected output %q", string(b))
	}
}

// TestAssembleSeqInvalid 测试 FileID 混入导致错误
func TestAssembleSeqInvalid(t *testing.T) {
	a, _ := New(nil)
	spans := []contract.SpanResult{{FileID: "a", From: 0, To: 0, Output: "x"}}
	_, err := a.Assemble(context.Background(), "b", spans)
	if err == nil || err != contract.ErrSeqInvalid {
		t.Fatalf("expect ErrSeqInvalid, got %v", err)
	}
}

// TestAssembleOverlap 测试索引逆序或重叠
func TestAssembleOverlap(t *testing.T) {
	a, _ := New(nil)
	spans := []contract.SpanResult{
		{FileID: "f", From: 1, To: 2, Output: "a"},
		{FileID: "f", From: 2, To: 3, Output: "b"},
	}
	if _, err := a.Assemble(context.Background(), "f", spans); err != contract.ErrSeqInvalid {
		t.Fatalf("expect ErrSeqInvalid, got %v", err)
	}
}

// TestAssembleEmpty 测试空输入
func TestAssembleEmpty(t *testing.T) {
	a, _ := New(nil)
	r, err := a.Assemble(context.Background(), "f", nil)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	data, _ := io.ReadAll(r)
	if len(data) != 0 {
		t.Fatalf("expect empty, got %q", string(data))
	}
}
