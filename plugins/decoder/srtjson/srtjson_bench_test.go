package srtjson

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"llmspt/pkg/contract"
)

// BenchmarkDecode 基准测试 Decoder.Decode。
func BenchmarkDecode(b *testing.B) {
	dec, _ := New(nil)
	sizes := []int{100, 1000, 5000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			raw := makeRaw(n)
			tgt := contract.Target{FileID: "f", From: 0, To: contract.Index(n - 1)}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := dec.Decode(ctx, tgt, raw); err != nil {
					b.Fatalf("解码失败: %v", err)
				}
			}
		})
	}
}

func makeRaw(n int) contract.Raw {
	type item struct {
		ID   int    `json:"id"`
		Text string `json:"text"`
	}
	arr := make([]item, n)
	for i := 0; i < n; i++ {
		arr[i] = item{ID: i, Text: "hello"}
	}
	b, _ := json.Marshal(arr)
	return contract.Raw{Text: string(b)}
}
