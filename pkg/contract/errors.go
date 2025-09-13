package contract

import "errors"

// Writer/路径相关最小错误分类。
var (
	// ErrPathInvalid: 目标标识映射为无效/越界路径（例如绝对路径或 '..' 逃逸）。
	ErrPathInvalid = errors.New("path invalid")
	// ErrBudgetExceeded: 预算或配额不足（如 token 预算、上游配额）。
	ErrBudgetExceeded = errors.New("budget exceeded")
	// ErrInvariantViolation: 领域不变量违例（通用哨兵）。
	ErrInvariantViolation = errors.New("invariant violation")
)
