package diag

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// 级别定义
type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return "info"
	}
}

// Logger 为最小结构化日志器：单行 JSON 输出到 stderr；支持级别与采样（info/warn）。
type Logger struct {
	corrID string
	level  Level
	sink   *RotatingFile
	mu     sync.Mutex
}

// NewLogger 通过配置的 level 初始化，并将日志写入默认路径 output/log，10m 轮转。
func NewLogger(corrID, level string) *Logger {
	lvl := parseLevel(strings.TrimSpace(level))
	sink := NewRotatingFile("logs", 10*1024*1024)
	return &Logger{corrID: corrID, level: lvl, sink: sink}
}

func parseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return Debug
	case "warn":
		return Warn
	case "error":
		return Error
	default:
		return Info
	}
}

// Event 为标准事件结构。
type Event struct {
	Level  string            `json:"level"`
	TS     string            `json:"ts"`
	CorrID string            `json:"corr_id"`
	Comp   string            `json:"comp"`
	Stage  string            `json:"stage"` // start|finish|error
	Code   string            `json:"code,omitempty"`
	DurMS  int64             `json:"dur_ms,omitempty"`
	Count  int64             `json:"count,omitempty"`
	FileID string            `json:"file_id,omitempty"`
	Batch  string            `json:"batch_id,omitempty"`
	Msg    string            `json:"msg"`
	KV     map[string]string `json:"kv,omitempty"`
}

// log 以最小开销写出事件，遵循级别与采样。
func (l *Logger) log(lv Level, ev Event) {
	if lv < l.level {
		return
	}
	// error 永不采样
	ev.Level = lv.String()
	ev.TS = NowUTC()
	ev.CorrID = l.corrID
	b, _ := json.Marshal(ev)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sink == nil {
		// 后备：写 stderr
		_, _ = os.Stderr.Write(append(b, '\n'))
		return
	}
	if err := l.sink.WriteLine(b); err != nil {
		fmt.Fprintf(os.Stderr, "logger sink error: %v\n", err)
		_, _ = os.Stderr.Write(append(b, '\n'))
	}
}

// Start 记录 start 事件；返回计时器用于 Finish。
func (l *Logger) Start(comp, msg string) *Timer {
	l.log(Info, Event{Comp: comp, Stage: "start", Msg: msg})
	return &Timer{l: l, comp: comp, t0: time.Now()}
}

// StartWith 记录带 file_id/batch_id 的 start。
func (l *Logger) StartWith(comp, msg, fileID, batch string) *Timer {
	l.log(Info, Event{Comp: comp, Stage: "start", FileID: fileID, Batch: batch, Msg: msg})
	return &Timer{l: l, comp: comp, fileID: fileID, batch: batch, t0: time.Now()}
}

// StartWithKV 记录带 file_id/batch_id 与键值的 start。
func (l *Logger) StartWithKV(comp, msg, fileID, batch string, kv map[string]string) *Timer {
	l.log(Info, Event{Comp: comp, Stage: "start", FileID: fileID, Batch: batch, Msg: msg, KV: kv})
	return &Timer{l: l, comp: comp, fileID: fileID, batch: batch, t0: time.Now()}
}

// Error 记录 error 事件（不采样）。
func (l *Logger) Error(comp, code, msg string, durSince *time.Time) {
	var dur int64
	if durSince != nil {
		dur = time.Since(*durSince).Milliseconds()
	}
	l.log(Error, Event{Comp: comp, Stage: "error", Code: code, DurMS: dur, Msg: msg})
}

// ErrorWith 支持 file_id/batch_id。
func (l *Logger) ErrorWith(comp, code, msg string, durSince *time.Time, fileID, batch string) {
	var dur int64
	if durSince != nil {
		dur = time.Since(*durSince).Milliseconds()
	}
	l.log(Error, Event{Comp: comp, Stage: "error", Code: code, DurMS: dur, Msg: msg, FileID: fileID, Batch: batch})
}

// ErrorWithKV 支持附带键值对（例如 HTTP 状态码、上游错误片段）。
func (l *Logger) ErrorWithKV(comp, code, msg string, durSince *time.Time, fileID, batch string, kv map[string]string) {
    var dur int64
    if durSince != nil {
        dur = time.Since(*durSince).Milliseconds()
    }
    l.log(Error, Event{Comp: comp, Stage: "error", Code: code, DurMS: dur, Msg: msg, FileID: fileID, Batch: batch, KV: kv})
}

// InfoFinish 在已有起点的情况下记录 finish。
func (l *Logger) InfoFinish(comp, msg string, start time.Time, count int64) {
	l.log(Info, Event{Comp: comp, Stage: "finish", DurMS: time.Since(start).Milliseconds(), Count: count, Msg: msg})
}

// Timer 用于 start→finish 计时。
type Timer struct {
	l      *Logger
	comp   string
	fileID string
	batch  string
	t0     time.Time
}

// Finish 记录 finish；可选 count。
func (t *Timer) Finish(msg string, count int64) {
	if t == nil || t.l == nil {
		return
	}
	// 带上 file_id/batch_id
	t.l.log(Info, Event{Comp: t.comp, Stage: "finish", DurMS: time.Since(t.t0).Milliseconds(), Count: count, FileID: t.fileID, Batch: t.batch, Msg: msg})
}

// DebugStart 输出调试级别的“start”类事件（仅在 level=debug 时生效）。
func (l *Logger) DebugStart(comp, msg, fileID, batch string, kv map[string]string) {
	l.log(Debug, Event{Comp: comp, Stage: "start", FileID: fileID, Batch: batch, Msg: msg, KV: kv})
}
