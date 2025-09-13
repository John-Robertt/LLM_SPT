package contract

// FileID: 逻辑文档ID（通常为路径，需规范化，跨平台一致）。
type FileID string

// Index: 单文件内稳定递增的索引（0..n-1）。
type Index int64

// Meta: 可选的轻量元信息；核心流程不读取其键值。
type Meta map[string]string

// Record: 原子输入片段（不可跨文件）。
// 约束：
// - FileID 一致；
// - Index 自 0 严格递增；
// - Text 为最小必需文本（经 CRLF→LF 归一），不做业务性清洗。
type Record struct {
	Index  Index
	FileID FileID
	Text   string
	Meta   Meta // 可为 nil
}

// Batch: 上下文批。保证同源文件、按 Index 严格升序，形如
// [L 上下文][Target 区间][R 上下文]。仅 Target 区间需要产出结果；
// 上下文仅用于提供语境。
type Batch struct {
	FileID FileID
	// BatchIndex: 同一 FileID 内的批序（0..n-1，严格递增）。
	// 仅用于跨批顺序恢复与装配门闩；不影响批内 Records 顺序与语义。
	BatchIndex int64
	Records    []Record
	TargetFrom Index // 闭区间下界（全局 Index）
	TargetTo   Index // 闭区间上界（全局 Index）
}

// 预留：结果类型在架构文档中以 SpanResult 形式出现；
// 当前阶段不在此处定义冗余类型，避免与后续契约冲突。
