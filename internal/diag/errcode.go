package diag

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"llmspt/pkg/contract"
)

// Code 是最小错误分类代码。
// 仅用于日志/指标汇总，与退出码解耦。
type Code string

const (
	CodeUnknown   Code = "unknown"
	CodeNetwork   Code = "network"
	CodeProtocol  Code = "protocol"
	CodeInvariant Code = "invariant"
	CodeBudget    Code = "budget"
	CodeCancel    Code = "cancel"
	CodeIO        Code = "io"
)

// Classify 将错误归为最小分类。
// 说明：仅依赖哨兵错误与标准库错误类型，不做字符串匹配。
func Classify(err error) Code {
	if err == nil {
		return CodeUnknown
	}
	// 取消/超时优先
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CodeCancel
	}
	// 预算/配额
	if errors.Is(err, contract.ErrBudgetExceeded) || errors.Is(err, contract.ErrRateLimited) {
		return CodeBudget
	}
	// 协议/解码
	if errors.Is(err, contract.ErrResponseInvalid) {
		return CodeProtocol
	}
	// 不变量
	if errors.Is(err, contract.ErrInvariantViolation) ||
		errors.Is(err, contract.ErrInvalidInput) ||
		errors.Is(err, contract.ErrSeqInvalid) ||
		errors.Is(err, contract.ErrPathInvalid) {
		return CodeInvariant
	}
	// I/O
	var perr *os.PathError
	if errors.As(err, &perr) {
		return CodeIO
	}
	// 网络（连接/超时等）
	var nerr net.Error
	if errors.As(err, &nerr) {
		return CodeNetwork
	}
	return CodeUnknown
}

// NowUTC 返回 RFC3339 UTC 时间字符串（用于结构化日志字段 ts）。
func NowUTC() string { return time.Now().UTC().Format(time.RFC3339) }
