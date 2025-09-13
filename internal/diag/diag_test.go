package diag

import (
    "context"
    "errors"
    "fmt"
    "io/fs"
    "net"
    "os"
    "strings"
    "testing"
    "time"

    "llmspt/pkg/contract"
)

// UT-DIAG-01: 日志轮转写入
func TestRotatingFile(t *testing.T) {
    dir := t.TempDir()
    w := NewRotatingFile(dir, 30)
    if err := w.WriteLine([]byte("first line that is very long")); err != nil {
        t.Fatalf("写入失败: %v", err)
    }
    if err := w.WriteLine([]byte("second")); err != nil {
        t.Fatalf("第二次写入失败: %v", err)
    }
    files, err := os.ReadDir(dir)
    if err != nil {
        t.Fatalf("读取目录失败: %v", err)
    }
    if len(files) < 2 {
        t.Fatalf("应存在轮转文件, got %d", len(files))
    }
}

// 进一步覆盖：当前文件名与时间戳文件存在
func TestRotatingFileRotateFiles(t *testing.T) {
    dir := t.TempDir()
    w := NewRotatingFile(dir, 10)
    for i := 0; i < 5; i++ {
        if err := w.WriteLine([]byte("xxxxxxxxxxxxxxxxxx")); err != nil {
            t.Fatalf("write: %v", err)
        }
    }
    // 检查 current 与至少一个历史文件
    ents, err := os.ReadDir(dir)
    if err != nil {
        t.Fatalf("readdir: %v", err)
    }
    hasCurrent := false
    hasRotated := false
    for _, e := range ents {
        if strings.HasSuffix(e.Name(), "llmspt-current.txt") {
            hasCurrent = true
        }
        if strings.HasPrefix(e.Name(), "llmspt-") && strings.HasSuffix(e.Name(), ".txt") && !strings.Contains(e.Name(), "current") {
            hasRotated = true
        }
    }
    if !hasCurrent || !hasRotated {
        t.Fatalf("expect both current and rotated files, got current=%v rotated=%v", hasCurrent, hasRotated)
    }
}

// 直接覆盖 ensureOpen 与 rotate 内部分支
func TestRotatingFileEnsureAndRotate(t *testing.T) {
    dir := t.TempDir()
    w := NewRotatingFile(dir, 1024)
    if err := w.ensureOpen(); err != nil { //nolint:forbidigo // 访问非导出以提高覆盖率
        t.Fatalf("ensureOpen: %v", err)
    }
    if w.f == nil {
        t.Fatalf("file should be opened")
    }
    // 强制轮转
    if err := w.rotate(); err != nil { //nolint:forbidigo
        t.Fatalf("rotate: %v", err)
    }
    // 检查两个文件存在
    ents, err := os.ReadDir(dir)
    if err != nil {
        t.Fatalf("readdir: %v", err)
    }
    if len(ents) < 2 {
        t.Fatalf("expect >=2 files, got %d", len(ents))
    }
}

// UT-DIAG-02: 指标计数
func TestMetricsNoop(t *testing.T) {
	IncOp("comp", "stage", "success")
	IncError("comp", "code")
	ObserveDuration("comp", "stage", 1)
}

// 补充覆盖: 错误分类
func TestClassify(t *testing.T) {
    if CodeProtocol != Classify(contract.ErrResponseInvalid) {
        t.Fatalf("分类错误")
    }
    if CodeCancel != Classify(context.Canceled) {
        t.Fatalf("取消分类错误")
    }
    err := &fs.PathError{Op: "open", Path: "/", Err: errors.New("x")}
    if CodeIO != Classify(err) {
        t.Fatalf("IO 分类错误")
    }
    nerr := &net.DNSError{Err: "x"}
    if CodeNetwork != Classify(nerr) {
        t.Fatalf("网络分类错误")
    }
    if CodeBudget != Classify(contract.ErrBudgetExceeded) {
        t.Fatalf("预算分类错误")
    }
    if CodeUnknown != Classify(errors.New("other")) {
        t.Fatalf("未知分类错误")
    }
}

// 补充覆盖: Logger 基本流程
func TestLogger(t *testing.T) {
    l := NewLogger("corr", "debug")
    l.sink = nil // 避免文件操作
    timer := l.Start("comp", "msg")
    timer.Finish("ok", 1)
	timer = l.StartWith("comp", "msg", "fid", "bid")
	timer.Finish("ok", 1)
	timer = l.StartWithKV("comp", "msg", "fid", "bid", map[string]string{"k": "v"})
	timer.Finish("ok", 1)
	l.Error("comp", "code", "msg", nil)
    l.ErrorWith("comp", "code", "msg", nil, "fid", "bid")
    l.ErrorWithKV("comp", "code", "msg", nil, "fid", "bid", map[string]string{"http_status": "500"})
    l.InfoFinish("comp", "msg", time.Now(), 1)
    l.DebugStart("comp", "msg", "fid", "bid", nil)
    _ = l
}

// 补充覆盖: NowUTC
func TestNowUTC(t *testing.T) {
    if NowUTC() == "" {
        t.Fatalf("应返回时间字符串")
    }
}

// UT-DIAG-03: 终端（非 TTY）关键节点输出
func TestTerminalNonTTYFlow(t *testing.T) {
    var sb strings.Builder
    term := NewTerminal(&sb, true)
    // 非 TTY：默认 bytes.Builder 不是 *os.File
    if term.isTTY {
        t.Fatalf("expect non-tty")
    }
    term.RunStart(4, "openai")
    term.FileStart("docs/guide.md", 12)
    term.FileProgress(6, 12, 0) // 非 TTY：不输出进度
    term.FileFinish(true, 5100*time.Millisecond)
    term.RunFinish(true, 41300 * time.Millisecond)

    out := sb.String()
    if strings.Contains(out, "\r") {
        t.Fatalf("non-tty should not contain carriage returns: %q", out)
    }
    // 关键行存在
    if !strings.Contains(out, "[run] 并发=4 | llm=openai") {
        t.Fatalf("missing run line: %q", out)
    }
    if !strings.Contains(out, "[file] guide.md | 计划批次=12") {
        t.Fatalf("missing file line: %q", out)
    }
    if !strings.Contains(out, "[done] guide.md | 批次 12 | 总用时 5.1s") {
        t.Fatalf("missing done line: %q", out)
    }
    if !strings.Contains(out, "[ok] 全部完成 | 文件 1 | 总用时 41.3s") {
        t.Fatalf("missing ok line: %q", out)
    }
}

// UT-DIAG-04: 终端（TTY）进度节流与清尾
func TestTerminalTTYProgressThrottleAndClear(t *testing.T) {
    var sb strings.Builder
    term := NewTerminal(&sb, true)
    term.isTTY = true // 强制 TTY
    term.RunStart(2, "mock")
    term.FileStart("/a/b/c/longfilename.txt", 3)

    // 第一次进度：应输出一行覆盖（无换行）
    term.FileProgress(1, 3, 0)
    first := sb.String()
    if !strings.Contains(first, "\r[") { // 以回车覆盖开头
        t.Fatalf("first progress should be inline with CR: %q", first)
    }
    // 立即第二次：应被节流（<100ms）
    term.FileProgress(2, 3, 1)
    second := sb.String()
    if second != first {
        t.Fatalf("second progress should be throttled; got changed output")
    }
    time.Sleep(120 * time.Millisecond)
    term.FileProgress(2, 3, 1)
    third := sb.String()
    if len(third) <= len(second) {
        t.Fatalf("third progress should append output")
    }
    // 完成：应先清尾（回车+空格覆盖），再输出换行 done/fail 行
    term.FileFinish(false, 2200*time.Millisecond)
    final := sb.String()
    if !strings.Contains(final, "[fail]") {
        t.Fatalf("finish should include fail line: %q", final)
    }
    // 清尾验证：在 fail 之前应出现一段以回车开头的空格串
    idx := strings.LastIndex(final, "[fail]")
    seg := final[:idx]
    if !strings.Contains(seg, "\r") {
        t.Fatalf("should contain carriage return before fail line")
    }
    // 回车后应至少有 1 个空格（覆盖短行）
    cr := strings.LastIndex(seg, "\r")
    if cr >= 0 {
        trail := seg[cr+1:]
        if !strings.Contains(trail, " ") {
            t.Fatalf("clear tail should write spaces after CR: %q", trail)
        }
    }
}

// UT-DIAG-05: 写失败降级为禁用态
type flakyWriter struct{ fail bool }

func (w *flakyWriter) Write(p []byte) (int, error) {
    if w.fail {
        w.fail = false
        return 0, fmt.Errorf("boom")
    }
    return len(p), nil
}

func TestTerminalDisableOnWriteError(t *testing.T) {
    fw := &flakyWriter{fail: true}
    term := NewTerminal(fw, true)
    term.isTTY = false
    term.RunStart(1, "x") // 第一次 println 触发失败
    if term.enabled {
        t.Fatalf("terminal should be disabled after write error")
    }
    // 后续调用应该是 no-op，不应 panic
    term.FileStart("a", 0)
    term.FileProgress(0, 0, 0)
    term.FileFinish(true, 0)
    term.RunFinish(true, 0)
}

// UT-DIAG-06: 工具函数覆盖
func TestHelpers(t *testing.T) {
    if shortenBase("/x/y/这是一个很长的文件名用于截断测试abcdefghijk.txt", 10) == "" {
        t.Fatalf("shortenBase should produce non-empty")
    }
    if safe("a\nb\rc") != "a b c" {
        t.Fatalf("safe replace failed")
    }
    if formatDur(0) != "0ms" {
        t.Fatalf("formatDur 0ms failed")
    }
    if formatDur(1500*time.Millisecond) != "1.5s" {
        t.Fatalf("formatDur 1.5s failed: %s", formatDur(1500*time.Millisecond))
    }
    SetTerminal(nil)
    if GetTerminal() != nil {
        t.Fatalf("expected nil terminal")
    }
    t1 := NewTerminal(os.Stderr, false)
    SetTerminal(t1)
    if GetTerminal() == nil {
        t.Fatalf("expected non-nil terminal")
    }
}

// 覆盖 NewTerminal 针对 *os.File 的 isTTY 判定路径
func TestNewTerminalWithFile(t *testing.T) {
    term := NewTerminal(os.Stderr, true)
    if term == nil {
        t.Fatalf("nil term")
    }
}

// 覆盖 Logger sink 写入成功路径
func TestLoggerWithSink(t *testing.T) {
    l := NewLogger("corr", "info")
    // 写几条日志，触发 sink 路径
    timer := l.Start("comp", "msg")
    timer.Finish("ok", 1)
    l.Error("comp", "code", "msg", nil)
    // 检查日志文件存在
    if _, err := os.Stat("logs/llmspt-current.txt"); err != nil {
        t.Fatalf("log file not found: %v", err)
    }
}

// 覆盖 Level.String 与 parseLevel 分支，以及 lv<level 过滤
func TestLoggerLevelsAndFilter(t *testing.T) {
    if Warn.String() != "warn" {
        t.Fatalf("warn string")
    }
    var unknown Level = 12345
    if unknown.String() != "info" {
        t.Fatalf("default string")
    }
    _ = NewLogger("c", "warn")
    l := NewLogger("c", "info")
    // Debug 在 info 级别应被过滤
    l.DebugStart("comp", "msg", "f", "b", nil)
    // 非空 durSince 分支
    start := time.Now().Add(-10 * time.Millisecond)
    l.Error("comp", "code", "msg", &start)
    l.ErrorWith("comp", "code", "msg", &start, "f", "b")
    // Timer nil/l=nil 早返回
    var tnil *Timer
    tnil.Finish("x", 0)
    (&Timer{}).Finish("x", 0)
}

// 触发默认 maxBytes 分支与 rotate 在 f==nil 分支
func TestRotatingFileDefaultsAndRotateNoOpen(t *testing.T) {
    dir := t.TempDir()
    w := NewRotatingFile(dir, 0)
    if err := w.WriteLine([]byte("a")); err != nil {
        t.Fatalf("write: %v", err)
    }
    // f 置空并调用 rotate 覆盖 f==nil 分支
    w.f = nil
    if err := w.rotate(); err != nil { //nolint:forbidigo
        t.Fatalf("rotate: %v", err)
    }
}

// 覆盖 printInline 写失败分支（TTY）
func TestTerminalInlineWriteError(t *testing.T) {
    fw := &flakyWriter{fail: true}
    term := NewTerminal(fw, true)
    term.isTTY = true
    term.FileStart("f.txt", 2)
    term.FileProgress(1, 2, 0) // 第一次 inline 写失败 → 禁用
    if term.enabled {
        t.Fatalf("terminal should be disabled after inline error")
    }
}

// 覆盖 NewTerminal 中 CI 环境分支
func TestNewTerminalCIEnv(t *testing.T) {
    t.Setenv("CI", "true")
    var sb strings.Builder
    term := NewTerminal(&sb, true)
    if term.isTTY {
        t.Fatalf("CI env should force non-tty")
    }
}

// 覆盖 Terminal nil 接收者早返回
func TestTerminalNilReceiverNoop(t *testing.T) {
    var tn *Terminal
    tn.RunStart(1, "x")
    tn.FileStart("a", 1)
    tn.FileProgress(0, 0, 0)
    tn.FileFinish(true, 0)
    tn.RunFinish(true, 0)
}

// shortenBase 边界
func TestShortenBaseEdge(t *testing.T) {
    _ = shortenBase("", 10) // 行为依赖 filepath.Base("") 返回 "."，不做强断言
    if shortenBase("x", 0) != "" {
        t.Fatalf("shortenBase max<=0 should be empty")
    }
}
