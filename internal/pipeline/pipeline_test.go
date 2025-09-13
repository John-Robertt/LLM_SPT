package pipeline

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"llmspt/internal/diag"
	"llmspt/pkg/contract"
)

// 通用桩件 ----------------------------------------------------
type stubReader struct{}

func (stubReader) Iterate(ctx context.Context, roots []string, yield func(contract.FileID, io.ReadCloser) error) error {
	return yield(contract.FileID("f"), io.NopCloser(strings.NewReader("data")))
}

type stubSplitter struct{}

func (stubSplitter) Split(ctx context.Context, fileID contract.FileID, r io.Reader) ([]contract.Record, error) {
	return []contract.Record{{Index: 0, FileID: fileID, Text: "hi"}}, nil
}

type stubBatcher struct{}

func (stubBatcher) Make(ctx context.Context, records []contract.Record, limit contract.BatchLimit) ([]contract.Batch, error) {
	return []contract.Batch{{FileID: "f", BatchIndex: 0, Records: records, TargetFrom: 0, TargetTo: 0}}, nil
}

type stubPB struct{ overhead int }

func (s stubPB) Build(ctx context.Context, b contract.Batch) (contract.Prompt, error) {
	return nil, nil
}
func (s stubPB) EstimateOverheadTokens(est contract.TokenEstimator) int { return s.overhead }

type stubLLM struct{}

func (stubLLM) Invoke(ctx context.Context, b contract.Batch, p contract.Prompt) (contract.Raw, error) {
	return contract.Raw{Text: "raw"}, nil
}

type stubDecoder struct {
	fail   bool
	called int
}

func (d *stubDecoder) Decode(ctx context.Context, tgt contract.Target, raw contract.Raw) ([]contract.SpanResult, error) {
	d.called++
	if d.fail && d.called == 1 {
		return nil, contract.ErrResponseInvalid
	}
	return []contract.SpanResult{{FileID: tgt.FileID, From: tgt.From, To: tgt.To, Output: "ok"}}, nil
}

type stubAssembler struct{}

func (stubAssembler) Assemble(ctx context.Context, fid contract.FileID, spans []contract.SpanResult) (io.Reader, error) {
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(s.Output)
	}
	return strings.NewReader(sb.String()), nil
}

type stubWriter struct{ out strings.Builder }

func (w *stubWriter) Write(ctx context.Context, id contract.ArtifactID, r io.Reader) error {
    // 测试仅关注主工件输出；忽略 JSONL 边车写入
    if strings.HasSuffix(string(id), ".jsonl") {
        _, _ = io.Copy(io.Discard, r)
        return nil
    }
    b, _ := io.ReadAll(r)
    w.out.Write(b)
    return nil
}

// UT-PIP-01: 预算不足
func TestRunBudgetExceeded(t *testing.T) {
	comp := Components{
		Reader: stubReader{}, Splitter: stubSplitter{}, Batcher: stubBatcher{},
		PromptBuilder: stubPB{overhead: 10}, LLM: stubLLM{}, Decoder: &stubDecoder{},
		Assembler: stubAssembler{}, Writer: &stubWriter{},
	}
	set := Settings{Inputs: []string{"in"}, Concurrency: 1, MaxTokens: 5, MaxRetries: 0}
	err := Run(context.Background(), comp, set, nil)
	if !errors.Is(err, contract.ErrBudgetExceeded) {
		t.Fatalf("应返回预算错误, got %v", err)
	}
}

// UT-PIP-02: 协议错误重试
func TestRunRetryDecode(t *testing.T) {
	dec := &stubDecoder{fail: true}
	w := &stubWriter{}
	comp := Components{
		Reader: stubReader{}, Splitter: stubSplitter{}, Batcher: stubBatcher{},
		PromptBuilder: stubPB{overhead: 0}, LLM: stubLLM{}, Decoder: dec,
		Assembler: stubAssembler{}, Writer: w,
	}
	set := Settings{Inputs: []string{"in"}, Concurrency: 1, MaxTokens: 100, MaxRetries: 1}
	logger := diag.NewLogger("c", "debug")
	if err := Run(context.Background(), comp, set, logger); err != nil {
		t.Fatalf("运行失败: %v", err)
	}
	if dec.called != 2 {
		t.Fatalf("应重试一次, 实际 %d", dec.called)
	}
	if w.out.String() != "ok" {
		t.Fatalf("输出错误: %s", w.out.String())
	}
}
