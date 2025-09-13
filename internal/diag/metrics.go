package diag

// 最小指标接口（无导出实现，默认 no-op）。
// 名称参考 5.3.4：
// - op_total{comp,stage,result}
// - error_total{comp,code}
// - op_duration_ms{comp,stage}

// IncOp 累加操作计数（result=success|error）。
func IncOp(comp, stage, result string) {
	// 保持最小 no-op；适配层可通过替换实现导出。
}

// IncError 按分类累加错误计数。
func IncError(comp, code string) {
	// 保持最小 no-op；适配层可通过替换实现导出。
}

// ObserveDuration 记录阶段耗时（毫秒）。
func ObserveDuration(comp, stage string, durMS int64) {
	// 保持最小 no-op；适配层可通过替换实现导出。
}
