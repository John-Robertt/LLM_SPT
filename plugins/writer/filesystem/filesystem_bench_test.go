package filesystem

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"llmspt/pkg/contract"
)

// BenchmarkWrite 基准测试 Writer.Write。不同输入尺寸下测量写入性能。
func BenchmarkWrite(b *testing.B) {
	sizes := []int{1024, 1024 * 1024}
	for _, sz := range sizes {
		b.Run(fmt.Sprintf("size=%d", sz), func(b *testing.B) {
			data := bytes.Repeat([]byte("a"), sz)
			dir := b.TempDir()
			w, err := New(&Options{OutputDir: dir})
			if err != nil {
				b.Fatalf("创建 Writer 失败: %v", err)
			}
			id := contract.ArtifactID("out.txt")
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := w.Write(ctx, id, bytes.NewReader(data)); err != nil {
					b.Fatalf("写入失败: %v", err)
				}
			}
		})
	}
}
