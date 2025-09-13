package testdata

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "llmspt/internal/config"
	"llmspt/internal/pipeline"
	"llmspt/pkg/contract"
)

// expectedOutput 根据输入文件与前缀构造期望输出。
func expectedOutput(t *testing.T, inPath, prefix string) string {
	f, err := os.Open(inPath)
	if err != nil {
		t.Fatalf("open input: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	var out strings.Builder
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		if i+2 > len(lines) {
			break
		}
		seq := lines[i]
		timeLine := lines[i+1]
		i += 2
		var texts []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			texts = append(texts, lines[i])
			i++
		}
		if len(texts) > 0 {
			texts[0] = prefix + ": " + texts[0]
		}
		out.WriteString(seq)
		out.WriteByte('\n')
		out.WriteString(timeLine)
		out.WriteByte('\n')
		for _, l := range texts {
			out.WriteString(l)
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
		i++
	}
	return out.String()
}

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

func runPipeline(t *testing.T, cfg cfgpkg.Config) error {
	comp, set, _, _, err := cfgpkg.Assemble(cfg)
	if err != nil {
		return err
	}
	return pipeline.Run(context.Background(), comp, set, nil)
}

func TestE2ESuccess(t *testing.T) {
	in := filepath.Join("files", "test-100-line.srt")
	outDir := t.TempDir()
	cfg := baseConfig(in, outDir)
	cfg.LLM = "mock"
	cfg.Provider["mock"] = cfgpkg.Provider{
		Client:  "mock",
		Options: json.RawMessage(`{"prefix":"DEBUG","response_mode":"translate_json_per_record"}`),
		Limits:  cfgpkg.Limits{RPM: 0, TPM: 0, MaxTokensPerReq: 0},
	}
	if err := runPipeline(t, cfg); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "test-100-line.srt"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := expectedOutput(t, in, "DEBUG")
	if string(got) != want {
		t.Fatalf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestE2EBudgetExceeded(t *testing.T) {
	in := filepath.Join("files", "test-100-line.srt")
	outDir := t.TempDir()
	cfg := baseConfig(in, outDir)
	cfg.MaxTokens = 1
	cfg.LLM = "mock"
	cfg.Provider["mock"] = cfgpkg.Provider{
		Client:  "mock",
		Options: json.RawMessage(`{"prefix":"DEBUG","response_mode":"translate_json_per_record"}`),
		Limits:  cfgpkg.Limits{RPM: 0, TPM: 0, MaxTokensPerReq: 0},
	}
	err := runPipeline(t, cfg)
	if err == nil || !strings.Contains(err.Error(), contract.ErrBudgetExceeded.Error()) {
		t.Fatalf("expect budget error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "test-100-line.srt")); err == nil {
		t.Fatalf("output file should not exist")
	}
}

func TestE2ERetry(t *testing.T) {
	in := filepath.Join("files", "test-100-line.srt")
	outDir := t.TempDir()
	logPath := filepath.Join(outDir, "flaky.log")
	cfg := baseConfig(in, outDir)
	cfg.LLM = "flaky"
	cfg.MaxRetries = 2
	cfg.Provider["flaky"] = cfgpkg.Provider{
		Client:  "flaky",
		Options: json.RawMessage(fmt.Sprintf(`{"prefix":"FLAKY","log_path":%q}`, logPath)),
		Limits:  cfgpkg.Limits{RPM: 0, TPM: 0, MaxTokensPerReq: 0},
	}
	if err := runPipeline(t, cfg); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "test-100-line.srt"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := expectedOutput(t, in, "FLAKY")
	if string(got) != want {
		t.Fatalf("output mismatch")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(lines) < 3 || lines[0] != "rate_limited" || lines[1] != "invalid_json" {
		t.Fatalf("unexpected log: %v", lines)
	}
}
