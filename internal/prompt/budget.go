package prompt

import "llmspt/pkg/contract"

// MakeEstimator 返回一个近似 token 估算器：tokens ≈ ceil(len(utf8_bytes)/bytesPerToken)。
// 当 bytesPerToken<=0 时采用默认 4。
func MakeEstimator(bytesPerToken int) contract.TokenEstimator {
	bpt := bytesPerToken
	if bpt <= 0 {
		bpt = 4
	}
	return func(s string) int {
		n := len([]byte(s))
		if n == 0 {
			return 0
		}
		return (n + bpt - 1) / bpt
	}
}

// EffectiveMaxTokens 计算预扣“固定提示开销”后的有效预算。
// 返回 (effectiveMax, overheadTokens)。若 maxTokens<=0，返回 (0,0)。
func EffectiveMaxTokens(pb contract.PromptBuilder, bytesPerToken int, maxTokens int) (int, int) {
	if maxTokens <= 0 {
		return 0, 0
	}
	est := MakeEstimator(bytesPerToken)
	overhead := pb.EstimateOverheadTokens(est)
	eff := maxTokens - overhead
	return eff, overhead
}
