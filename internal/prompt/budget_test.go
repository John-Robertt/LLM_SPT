package prompt

import (
	"context"
	"testing"

	"llmspt/pkg/contract"
)

// UT-PRM-01: 默认估算器
func TestMakeEstimatorDefault(t *testing.T) {
	est := MakeEstimator(0)
	if est("abcdef") != 2 { // 6 字节 -> 2 token
		t.Fatalf("估算错误")
	}
}

// UT-PRM-02: 0 token 输入
func TestEffectiveMaxTokensZero(t *testing.T) {
	pb := &mockPB{overhead: 0}
	eff, over := EffectiveMaxTokens(pb, 0, 0)
	if eff != 0 || over != 0 {
		t.Fatalf("应返回 0,0")
	}
}

type mockPB struct{ overhead int }

func (m *mockPB) Build(_ context.Context, b contract.Batch) (contract.Prompt, error) {
	return nil, nil
}

func (m *mockPB) EstimateOverheadTokens(est contract.TokenEstimator) int { return m.overhead }

// 补充覆盖: 非零开销
func TestEffectiveMaxTokensOverhead(t *testing.T) {
	pb := &mockPB{overhead: 5}
	eff, over := EffectiveMaxTokens(pb, 4, 10)
	if eff != 5 || over != 5 {
		t.Fatalf("预期 5,5 得到 %d,%d", eff, over)
	}
}
