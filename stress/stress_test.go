package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	cfgpkg "llmspt/internal/config"
	"llmspt/internal/pipeline"
)

// baseConfig 复制自 testdata，构造可运行的最小配置。
func baseConfig(input, outDir string) cfgpkg.Config {
	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{input}
	cfg.Components.Reader = "fs"
	cfg.Components.Splitter = "srt"
	cfg.Components.Batcher = "sliding"
	cfg.Components.Writer = "fs"
	cfg.Components.PromptBuilder = "translate"
	cfg.Components.Decoder = "srt"
	cfg.Components.Assembler = "linear"
	cfg.Logging.Level = "error"
	cfg.Provider = map[string]cfgpkg.Provider{}
	cfg.Options.Writer = json.RawMessage(fmt.Sprintf(`{"output_dir":%q,"atomic":false,"flat":true,"perm_file":0,"perm_dir":0,"buf_size":65536}`, outDir))
	cfg.Options.PromptBuilder = json.RawMessage(fmt.Sprintf(`{"inline_system_template":"","system_template_path":"","inline_glossary":"","glossary_path":%q}`, filepath.Join(filepath.Dir(input), "glossary.md")))
	return cfg
}

// runPipeline 执行完整流水线。
func runPipeline(t *testing.T, cfg cfgpkg.Config) error {
	comp, set, _, _, err := cfgpkg.Assemble(cfg)
	if err != nil {
		return err
	}
	return pipeline.Run(context.Background(), comp, set, nil)
}

// TestStress 在不同并发度下运行流水线并记录延迟统计。
func TestStress(t *testing.T) {
	srcInput := filepath.Join("..", "testdata", "files", "test-2283-line.srt")
	srcGloss := filepath.Join("..", "testdata", "files", "glossary.md")
	levels := []int{1, 8, 16, 32, 64}
	for _, conc := range levels {
		t.Run(fmt.Sprintf("concurrency_%d", conc), func(t *testing.T) {
			const runs = 5
			successes := 0
			latencies := make([]time.Duration, 0, runs)
			for i := 0; i < runs; i++ {
				dataDir, err := os.MkdirTemp(".", "data-")
				if err != nil {
					t.Fatalf("mkdata: %v", err)
				}
				defer os.RemoveAll(dataDir)
				in := filepath.Join(dataDir, "input.srt")
				gloss := filepath.Join(dataDir, "glossary.md")
				if err := copyFile(srcInput, in); err != nil {
					t.Fatalf("copy input: %v", err)
				}
				if err := copyFile(srcGloss, gloss); err != nil {
					t.Fatalf("copy glossary: %v", err)
				}
				relIn := filepath.Join(filepath.Base(dataDir), "input.srt")
				outDir, err := os.MkdirTemp(".", "out-")
				if err != nil {
					t.Fatalf("mkout: %v", err)
				}
				defer os.RemoveAll(outDir)
				relOut := filepath.Base(outDir)
				cfg := baseConfig(relIn, relOut)
				cfg.Concurrency = conc
				cfg.LLM = "mock"
				cfg.Provider["mock"] = cfgpkg.Provider{
					Client:  "mock",
					Options: json.RawMessage(`{"prefix":"STRESS","response_mode":"translate_json_per_record"}`),
					Limits:  cfgpkg.Limits{RPM: 0, TPM: 0, MaxTokensPerReq: 0},
				}
				start := time.Now()
				err = runPipeline(t, cfg)
				dur := time.Since(start)
				if err != nil {
					t.Errorf("run %d: %v", i, err)
					continue
				}
				successes++
				latencies = append(latencies, dur)
			}
			if successes == 0 {
				t.Fatalf("全部运行失败")
			}
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			var total time.Duration
			for _, d := range latencies {
				total += d
			}
			avg := total / time.Duration(len(latencies))
			idx := int(math.Ceil(float64(len(latencies))*0.95)) - 1
			if idx < 0 {
				idx = 0
			}
			p95 := latencies[idx]
			t.Logf("并发%d 成功率%.2f 平均%v 95%%延迟%v", conc, float64(successes)/float64(runs), avg, p95)
		})
	}
}

// copyFile 复制文件内容。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
