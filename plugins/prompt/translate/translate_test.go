package translate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"llmspt/pkg/contract"
)

// TestBuildDefault 测试默认模板构造
func TestBuildDefault(t *testing.T) {
	b, err := New(nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	batch := contract.Batch{Records: []contract.Record{
		{Index: 0, Text: "L"},
		{Index: 1, Text: "T"},
		{Index: 2, Text: "R"},
	}, TargetFrom: 1, TargetTo: 1}
	p, err := b.Build(context.Background(), batch)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cp, ok := p.(contract.ChatPrompt)
	if !ok || len(cp) != 3 {
		t.Fatalf("unexpected prompt %#v", p)
	}
	if !strings.Contains(cp[1].Content, "L") || !strings.Contains(cp[1].Content, "T") || !strings.Contains(cp[1].Content, "R") {
		t.Fatalf("window not built correctly: %s", cp[1].Content)
	}
	if !strings.Contains(cp[1].Content, "targets: [1]") {
		t.Fatalf("target ids missing: %s", cp[1].Content)
	}
}

// TestEstimateOverhead 测试开销估算
func TestEstimateOverhead(t *testing.T) {
	b, _ := New(&Options{InlineGlossary: "a:b"})
	est := b.EstimateOverheadTokens(func(s string) int { return len(s) })
	if est == 0 {
		t.Fatalf("expect positive estimate")
	}
}

// TestBuildWithGlossary 测试术语表追加
func TestBuildWithGlossary(t *testing.T) {
	b, _ := New(&Options{InlineGlossary: "t1"})
	batch := contract.Batch{Records: []contract.Record{{Index: 0, Text: "x"}}, TargetFrom: 0, TargetTo: 0}
	p, err := b.Build(context.Background(), batch)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cp := p.(contract.ChatPrompt)
	if !strings.Contains(cp[0].Content, "<glossary>") {
		t.Fatalf("glossary not appended")
	}
}

// TestNewInlineTemplateAndGlossary 内联模板和术语表
func TestNewInlineTemplateAndGlossary(t *testing.T) {
	b, err := New(&Options{InlineSystemTemplate: "sys", InlineGlossary: "g"})
	if err != nil || b.glos != "g" {
		t.Fatalf("new inline: %v", err)
	}
}

// TestNewTemplatePathGlossaryPath 从文件加载
func TestNewTemplatePathGlossaryPath(t *testing.T) {
	dir := t.TempDir()
	sys := filepath.Join(dir, "sys.txt")
	glos := filepath.Join(dir, "g.txt")
	os.WriteFile(sys, []byte("s"), 0o644)
	os.WriteFile(glos, []byte("g"), 0o644)
	b, err := New(&Options{SystemTemplatePath: sys, GlossaryPath: glos})
	if err != nil || b.glos != "g" {
		t.Fatalf("new file: %v", err)
	}
}

// TestNewTemplateParseError 模板解析失败
func TestNewTemplateParseError(t *testing.T) {
	if _, err := New(&Options{InlineSystemTemplate: "{{"}); err == nil {
		t.Fatalf("expect parse error")
	}
}
