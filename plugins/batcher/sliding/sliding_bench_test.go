package sliding

import (
	"context"
	"fmt"
	"testing"

	"llmspt/pkg/contract"
)

// BenchmarkMake 基准测试 Batcher.Make，不同记录数量下的表现。
func BenchmarkMake(b *testing.B) {
	sizes := []int{100, 1000, 5000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			recs := makeRecords(n)
			limit := contract.BatchLimit{MaxTokens: n * 10}
			bt := New(&Options{ContextRadius: 1})
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := bt.Make(ctx, recs, limit); err != nil {
					b.Fatalf("批处理失败: %v", err)
				}
			}
		})
	}
}

func makeRecords(n int) []contract.Record {
	recs := make([]contract.Record, n)
	for i := 0; i < n; i++ {
		recs[i] = contract.Record{Index: contract.Index(i), FileID: "f", Text: "hello world"}
	}
	return recs
}
