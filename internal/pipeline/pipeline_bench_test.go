package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"llmspt/pkg/contract"
	sliding "llmspt/plugins/batcher/sliding"
	djson "llmspt/plugins/decoder/srtjson"
	fsreader "llmspt/plugins/reader/filesystem"
	"llmspt/plugins/splitter/srt"
)

// mockLLM 模拟 LLM 调用，可设置固定延迟。
type mockLLM struct{ delay time.Duration }

func (m mockLLM) Invoke(ctx context.Context, b contract.Batch, p contract.Prompt) (contract.Raw, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return contract.Raw{}, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	type item struct {
		ID   int64  `json:"id"`
		Text string `json:"text"`
	}
	arr := make([]item, 0, b.TargetTo-b.TargetFrom+1)
	for _, r := range b.Records {
		if r.Index >= b.TargetFrom && r.Index <= b.TargetTo {
			arr = append(arr, item{ID: int64(r.Index), Text: r.Text})
		}
	}
	bs, _ := json.Marshal(arr)
	return contract.Raw{Text: string(bs)}, nil
}

// discardWriter 丢弃所有输出，避免磁盘开销。
type discardWriter struct{}

func (discardWriter) Write(ctx context.Context, id contract.ArtifactID, r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
}

// BenchmarkPipeline 测试完整流水线的性能。
func BenchmarkPipeline(b *testing.B) {
	testFile := filepath.Join("..", "..", "testdata", "files", "test-2283-line.srt")
	for _, c := range []int{1, runtime.NumCPU()} {
		b.Run(fmt.Sprintf("C=%d", c), func(b *testing.B) {
			reader := fsreader.New(nil)
			splitter := srt.New(nil)
			batcher := sliding.New(&sliding.Options{ContextRadius: 1})
			pb := stubPB{overhead: 0}
			llm := mockLLM{}
			dec, _ := djson.New(nil)
			asm := stubAssembler{}
			writer := discardWriter{}
			set := Settings{Inputs: []string{testFile}, Concurrency: c, MaxTokens: 4000, MaxRetries: 0}
			comp := Components{Reader: reader, Splitter: splitter, Batcher: batcher, PromptBuilder: pb, LLM: llm, Decoder: dec, Assembler: asm, Writer: writer}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := Run(ctx, comp, set, nil); err != nil {
					b.Fatalf("运行失败: %v", err)
				}
			}
		})
	}
}
