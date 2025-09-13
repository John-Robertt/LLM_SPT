package contract

// UpstreamError 用于承载 HTTP 上游错误的最小诊断信息。
// 实现方应提供可选的状态码与简短消息，便于 pipeline 记录结构化日志字段。
type UpstreamError interface {
    error
    UpstreamStatus() int
    UpstreamMessage() string
}

