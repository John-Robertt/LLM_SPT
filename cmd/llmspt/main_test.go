package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	cfgpkg "llmspt/internal/config"
	"llmspt/internal/diag"
	"llmspt/internal/pipeline"
)

func resetFlag(args []string) {
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	os.Args = args
}

func TestHasDash(t *testing.T) {
	if !hasDash([]string{"a", "-"}) {
		t.Errorf("expected true")
	}
	if hasDash([]string{"a", "b"}) {
		t.Errorf("expected false")
	}
}

func TestWriteConfig(t *testing.T) {
	cfg := cfgpkg.Defaults()
	dir := t.TempDir()
	file := filepath.Join(dir, "c.json")
	if err := writeConfig(file, cfg); err != nil {
		t.Fatalf("writeConfig file: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	if err := writeConfig("-", cfg); err != nil {
		t.Fatalf("writeConfig stdout: %v", err)
	}
	w.Close()
	os.Stdout = old
	r.Close()
}

func TestDumpConfig(t *testing.T) {
	cfg := cfgpkg.Defaults()
	devnull, _ := os.Open(os.DevNull)
	old := os.Stderr
	os.Stderr = devnull
	if err := dumpConfig(cfg); err != nil {
		t.Fatalf("dumpConfig: %v", err)
	}
	os.Stderr = old
	devnull.Close()
}

func TestRunInitConfig(t *testing.T) {
    dir := t.TempDir()
    cwd, _ := os.Getwd()
    os.Chdir(dir)
    defer os.Chdir(cwd)

    outDir := filepath.Join(dir, "out")
    resetFlag([]string{"llmspt", "--init-config", outDir})
    if code := run(); code != 0 {
        t.Fatalf("run return %d", code)
    }
    if _, err := os.Stat(filepath.Join(outDir, "config.json")); err != nil {
        t.Fatalf("config not generated: %v", err)
    }
}

func TestRunSuccess(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	b, _ := json.Marshal(cfg)
	t.Setenv("LLM_SPT_CONFIG_JSON", string(b))

	resetFlag([]string{"llmspt"})
	called := false
	orig := pipelineRun
	pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
		called = true
		return nil
	}
	defer func() { pipelineRun = orig }()

	if code := run(); code != 0 {
		t.Fatalf("run return %d", code)
	}
	if !called {
		t.Fatalf("pipelineRun not called")
	}
}

func TestRunWithConfigFile(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resetFlag([]string{"llmspt", "--config", path})
	called := false
	orig := pipelineRun
	pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
		called = true
		return nil
	}
	defer func() { pipelineRun = orig }()

	if code := run(); code != 0 {
		t.Fatalf("run return %d", code)
	}
	if !called {
		t.Fatalf("pipelineRun not called")
	}
}

func TestRunConfigFileNotFound(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	resetFlag([]string{"llmspt", "--config", "missing.json"})
	if code := run(); code != 3 {
		t.Fatalf("expect 3, got %d", code)
	}
}

func TestRunValidateError(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	cfg.LLM = ""
	cfg.Provider = map[string]cfgpkg.Provider{}
	b, _ := json.Marshal(cfg)
	t.Setenv("LLM_SPT_CONFIG_JSON", string(b))

	resetFlag([]string{"llmspt"})
	if code := run(); code != 3 {
		t.Fatalf("expect 3, got %d", code)
	}
}

func TestRunAssembleError(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	cfg.Options.Reader = json.RawMessage(`{"unknown":1}`)
	b, _ := json.Marshal(cfg)
	t.Setenv("LLM_SPT_CONFIG_JSON", string(b))

	resetFlag([]string{"llmspt"})
	if code := run(); code != 3 {
		t.Fatalf("expect 3, got %d", code)
	}
}

func TestRunPipelineError(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	b, _ := json.Marshal(cfg)
	t.Setenv("LLM_SPT_CONFIG_JSON", string(b))

	resetFlag([]string{"llmspt"})
	orig := pipelineRun
	pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
		return errors.New("boom")
	}
	defer func() { pipelineRun = orig }()

	if code := run(); code != 1 {
		t.Fatalf("expect 1, got %d", code)
	}
}

func TestRunInitConfigFileExists(t *testing.T) {
    dir := t.TempDir()
    cwd, _ := os.Getwd()
    os.Chdir(dir)
    defer os.Chdir(cwd)

    outDir := filepath.Join(dir, "out2")
    if err := os.MkdirAll(outDir, 0o755); err != nil {
        t.Fatalf("mkdir: %v", err)
    }
    dest := filepath.Join(outDir, "config.json")
    if err := os.WriteFile(dest, []byte("{}"), 0o644); err != nil {
        t.Fatalf("write existing: %v", err)
    }
    resetFlag([]string{"llmspt", "--init-config", outDir})
    if code := run(); code != 3 {
        t.Fatalf("expect 3, got %d", code)
    }
}

func TestRunCLIOverrides(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = nil
	cfg.LLM = ""
	b, _ := json.Marshal(cfg)
	t.Setenv("LLM_SPT_CONFIG_JSON", string(b))

	resetFlag([]string{"llmspt", "--llm", "mock", "--concurrency", "2", "--max-tokens", "100", "--max-retries", "1", "-"})
	called := false
	orig := pipelineRun
	pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
		called = true
		if set.Concurrency != 2 || set.MaxTokens != 100 || set.MaxRetries != 1 {
			t.Fatalf("cli overrides not applied")
		}
		return nil
	}
	defer func() { pipelineRun = orig }()

	if code := run(); code != 0 {
		t.Fatalf("run return %d", code)
	}
	if !called {
		t.Fatalf("pipelineRun not called")
	}
}

// 新增: --no-retry 应将 MaxRetries 显式覆盖为 0
// 使用 --max-retries=0 覆盖为禁用重试
func TestRunMaxRetriesZeroCLI(t *testing.T) {
    dir := t.TempDir()
    cwd, _ := os.Getwd()
    os.Chdir(dir)
    defer os.Chdir(cwd)

    cfg := cfgpkg.DefaultTemplateConfig()
    cfg.Inputs = []string{"-"}
    b, _ := json.Marshal(cfg)
    t.Setenv("LLM_SPT_CONFIG_JSON", string(b))

    resetFlag([]string{"llmspt", "--max-retries", "0"})
    orig := pipelineRun
    defer func() { pipelineRun = orig }()
    pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
        if set.MaxRetries != 0 {
            t.Fatalf("max-retries=0 not applied, got %d", set.MaxRetries)
        }
        return nil
    }
    if code := run(); code != 0 {
        t.Fatalf("run return %d", code)
    }
}

// 新增: LLM_SPT_NO_RETRY 优先级高于 LLM_SPT_MAX_RETRIES
// 使用 LLM_SPT_MAX_RETRIES=0 覆盖为禁用重试
func TestRunMaxRetriesZeroEnv(t *testing.T) {
    dir := t.TempDir()
    cwd, _ := os.Getwd()
    os.Chdir(dir)
    defer os.Chdir(cwd)

    cfg := cfgpkg.DefaultTemplateConfig()
    cfg.Inputs = []string{"-"}
    b, _ := json.Marshal(cfg)
    t.Setenv("LLM_SPT_CONFIG_JSON", string(b))
    t.Setenv("LLM_SPT_MAX_RETRIES", "0")

    resetFlag([]string{"llmspt"})
    orig := pipelineRun
    defer func() { pipelineRun = orig }()
    pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
        if set.MaxRetries != 0 {
            t.Fatalf("env max-retries=0 not applied, got %d", set.MaxRetries)
        }
        return nil
    }
    if code := run(); code != 0 {
        t.Fatalf("run return %d", code)
    }
}

func TestRunConfigFileEnv(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("LLM_SPT_CONFIG_FILE", path)

	resetFlag([]string{"llmspt"})
	called := false
	orig := pipelineRun
	pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
		called = true
		return nil
	}
	defer func() { pipelineRun = orig }()

	if code := run(); code != 0 {
		t.Fatalf("run return %d", code)
	}
	if !called {
		t.Fatalf("pipelineRun not called")
	}
}

func TestRunDefaultConfigFile(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile("config.json", b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resetFlag([]string{"llmspt"})
	called := false
	orig := pipelineRun
	pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
		called = true
		return nil
	}
	defer func() { pipelineRun = orig }()

	if code := run(); code != 0 {
		t.Fatalf("run return %d", code)
	}
	if !called {
		t.Fatalf("pipelineRun not called")
	}
}

func TestRunInitConfigDefault(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	resetFlag([]string{"llmspt", "--init-config"})
	if code := run(); code != 0 {
		t.Fatalf("run return %d", code)
	}
	if _, err := os.Stat("config.json"); err != nil {
		t.Fatalf("config not written: %v", err)
	}
}

func TestRunInitConfigDir(t *testing.T) {
    dir := t.TempDir()
    cwd, _ := os.Getwd()
    os.Chdir(dir)
    defer os.Chdir(cwd)

    outDir := filepath.Join(dir, "emit")
    resetFlag([]string{"llmspt", "--init-config", outDir})
    if code := run(); code != 0 {
        t.Fatalf("run return %d", code)
    }
    if _, err := os.Stat(filepath.Join(outDir, "config.json")); err != nil {
        t.Fatalf("config not generated: %v", err)
    }
    if _, err := os.Stat(filepath.Join(outDir, ".env")); err != nil {
        t.Fatalf(".env not generated: %v", err)
    }
}

func TestRunDebugProviderInfo(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	cfg := cfgpkg.DefaultTemplateConfig()
	cfg.Inputs = []string{"-"}
	cfg.LLM = "openai"
	cfg.Provider["openai"] = cfgpkg.Provider{
		Client:  "openai",
		Options: json.RawMessage(`{"base_url":"https://api.openai.com/v1","model":"gpt-4o-mini","api_key":"x","endpoint_path":"/chat/completions"}`),
	}
	b, _ := json.Marshal(cfg)
	t.Setenv("LLM_SPT_CONFIG_JSON", string(b))

	resetFlag([]string{"llmspt"})
	called := false
	orig := pipelineRun
	pipelineRun = func(ctx context.Context, comp pipeline.Components, set pipeline.Settings, logger *diag.Logger) error {
		called = true
		return nil
	}
	defer func() { pipelineRun = orig }()

	if code := run(); code != 0 {
		t.Fatalf("run return %d", code)
	}
	if !called {
		t.Fatalf("pipelineRun not called")
	}
}
