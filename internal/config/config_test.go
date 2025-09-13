package config

import (
	"testing"
)

// UT-CFG-01: 解析完整 config.json
func TestLoadJSON(t *testing.T) {
    cfg, err := LoadJSON("../../testdata/config/basic.json", nil)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.LLM != "gemini" {
		t.Fatalf("LLM 期望 gemini 实得 %s", cfg.LLM)
	}
	if len(cfg.Inputs) != 1 || cfg.Components.Reader != "fs" {
		t.Fatalf("字段映射错误: %+v", cfg)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("校验失败: %v", err)
	}
}

// UT-CFG-02: ENV 覆盖部分字段
func TestEnvOverlay(t *testing.T) {
	env := []string{
		"LLM_SPT_INPUTS=a,b",
		"LLM_SPT_CONCURRENCY=3",
		"LLM_SPT_LLM=mock",
		"LLM_SPT_COMPONENTS_READER=fs",
		"LLM_SPT_PROVIDER__mock__CLIENT=mock",
	}
	over, err := EnvOverlay(env)
	if err != nil {
		t.Fatalf("EnvOverlay 错误: %v", err)
	}
	if over.LLM != "mock" || over.Concurrency != 3 || len(over.Inputs) != 2 {
		t.Fatalf("覆盖结果不正确: %+v", over)
	}
}

// UT-CFG-03: 含非法字段
func TestLoadJSONUnknown(t *testing.T) {
	raw := []byte(`{"unknown":1}`)
	if _, err := LoadJSON("", raw); err == nil {
		t.Fatalf("应当返回错误")
	}
}

// 补充覆盖: splitComma 与 atoi
func TestSplitCommaAtoi(t *testing.T) {
	parts := splitComma("a, b , ,c")
	if len(parts) != 3 || parts[1] != "b" {
		t.Fatalf("splitComma 结果错误: %v", parts)
	}
	if v, err := atoi("10"); err != nil || v != 10 {
		t.Fatalf("atoi 失败: %v %d", err, v)
	}
}

// 补充覆盖: Defaults 与 cloneRaw
func TestDefaultsClone(t *testing.T) {
	d := Defaults()
	if d.Components.Reader != "fs" {
		t.Fatalf("默认 reader 错误: %v", d.Components.Reader)
	}
	src := []byte("abc")
	dst := cloneRaw(src)
	src[0] = 'x'
	if string(dst) != "abc" {
		t.Fatalf("cloneRaw 未复制")
	}
}

// 补充覆盖: Validate 错误分支
func TestValidateErrors(t *testing.T) {
	if err := Validate(Config{}); err == nil {
		t.Fatal("空配置应失败")
	}
	cfg := DefaultTemplateConfig()
	cfg.Inputs = []string{"-", "a"}
	if err := Validate(cfg); err == nil {
		t.Fatal("混用 '-' 应失败")
	}
	cfg = DefaultTemplateConfig()
	cfg.MaxTokens = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("MaxTokens<=0 应失败")
	}
	cfg = DefaultTemplateConfig()
	cfg.Provider = map[string]Provider{"mock": {Client: "", Limits: Limits{}}}
	if err := Validate(cfg); err == nil {
		t.Fatal("client 为空应失败")
	}
}
