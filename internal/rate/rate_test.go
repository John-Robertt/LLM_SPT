package rate

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// UT-RTE-01: 超过 RPM/TPM
func TestGateTryLimit(t *testing.T) {
	now := time.Unix(0, 0)
	clk := func() time.Time { return now }
	g := NewGate(map[LimitKey]Limits{"k": {RPM: 1, TPM: 10, MaxTokensPerReq: 5}}, clk)
	if !g.Try(Ask{Key: "k", Requests: 1, Tokens: 3}) {
		t.Fatalf("首次应通过")
	}
	if g.Try(Ask{Key: "k", Requests: 1, Tokens: 3}) {
		t.Fatalf("应因 RPM 拒绝")
	}
}

// UT-RTE-02: 取消上下文
func TestGateWaitCancel(t *testing.T) {
	now := time.Unix(0, 0)
	clk := func() time.Time { return now }
	g := NewGate(map[LimitKey]Limits{"k": {RPM: 1}}, clk)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if err := g.Wait(ctx, Ask{Key: "k", Requests: 2}); err == nil {
		t.Fatalf("应返回取消错误")
	}
}

// 补充覆盖: DeriveKeyFromProviderOptions
func TestDeriveKeyFromProviderOptions(t *testing.T) {
	os.Setenv("TEST_KEY", "abc")
	raw, _ := json.Marshal(map[string]any{"api_key_env": "TEST_KEY"})
	k, err := DeriveKeyFromProviderOptions("openai", raw)
	if err != nil || k == "" {
		t.Fatalf("派生失败: %v", err)
	}
	if _, err := DeriveKeyFromProviderOptions("openai", json.RawMessage(`{}`)); err == nil {
		t.Fatalf("缺少 key 应失败")
	}
}
