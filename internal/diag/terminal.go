package diag

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

// Terminal: 终端信息提示（非日志）。
// - 输出到提供的 io.Writer（默认建议 stderr）。
// - TTY: 单行 \r 覆盖；非 TTY: 关键节点分行打印。
// - 并发安全；写失败后进入禁用态为 no-op。
type Terminal struct {
    w       io.Writer
    enabled bool
    isTTY   bool

    // 运行期最小状态
    concurrency int
    llm         string
    filesDone   int
    runStart    time.Time

    // 当前文件
    curFileID    string // 短名（base + 截断）
    batchesTotal int
    batchesDone  int
    errCount     int

    // 输出控制
    lastLen   int
    lastFlush time.Time

    mu sync.Mutex
}

// 进程级终端（可选，全局设置后供 pipeline 旁路调用）。
var (
    termMu sync.RWMutex
    term   *Terminal
)

// SetTerminal 设置全局终端指针（nil 可清除）。
func SetTerminal(t *Terminal) { termMu.Lock(); term = t; termMu.Unlock() }

// GetTerminal 返回全局终端（可能为 nil）。
func GetTerminal() *Terminal { termMu.RLock(); defer termMu.RUnlock(); return term }

// NewTerminal 构造终端提示器。
// enabled=false 时总是 no-op。
func NewTerminal(w io.Writer, enabled bool) *Terminal {
    if w == nil {
        w = os.Stderr
    }
    t := &Terminal{w: w, enabled: enabled}
    // CI 环境视为非 TTY
    if os.Getenv("CI") != "" {
        t.isTTY = false
    } else if f, ok := w.(*os.File); ok {
        // 最小 TTY 判定：字符设备
        if fi, err := f.Stat(); err == nil {
            t.isTTY = fi.Mode()&os.ModeCharDevice != 0
        }
    }
    return t
}

// RunStart: 记录运行上下文（并发、LLM）。
func (t *Terminal) RunStart(concurrency int, llm string) {
    if t == nil { return }
    t.mu.Lock()
    defer t.mu.Unlock()
    if !t.enabled { return }
    t.concurrency = concurrency
    t.llm = llm
    t.filesDone = 0
    t.runStart = time.Now()
    // 起始提示
    if t.isTTY {
        t.println(fmt.Sprintf("[run] 并发=%d | llm=%s | 等待任务…", concurrency, safe(llm)))
    } else {
        t.println(fmt.Sprintf("[run] 并发=%d | llm=%s", concurrency, safe(llm)))
    }
}

// FileStart: 标记当前文件与计划批次。
func (t *Terminal) FileStart(fileID string, batchesTotal int) {
    if t == nil { return }
    t.mu.Lock()
    defer t.mu.Unlock()
    if !t.enabled { return }
    t.curFileID = shortenBase(fileID, 48)
    t.batchesTotal = batchesTotal
    t.batchesDone = 0
    t.errCount = 0
    if !t.isTTY { // 非 TTY 打点一行
        t.println(fmt.Sprintf("[file] %s | 计划批次=%d", t.curFileID, batchesTotal))
    }
}

// FileProgress: 周期性进度（≥100ms 节流）。
func (t *Terminal) FileProgress(done, total, errs int) {
    if t == nil { return }
    t.mu.Lock()
    defer t.mu.Unlock()
    if !t.enabled || !t.isTTY { return }
    // 合并状态
    t.batchesDone = done
    t.batchesTotal = total
    t.errCount = errs
    // 节流：100ms
    now := time.Now()
    if now.Sub(t.lastFlush) < 100*time.Millisecond {
        return
    }
    t.lastFlush = now
    // 单行覆盖
    line := fmt.Sprintf("[file] %s | 进度 %d/%d | 错误 %d | 并发 %d | 用时 %s",
        t.curFileID, t.batchesDone, t.batchesTotal, t.errCount, t.concurrency, formatSince(t.runStart))
    t.printInline(line)
}

// FileFinish: 完成当前文件（立即刷新并换行；FilesDone++）。
func (t *Terminal) FileFinish(ok bool, dur time.Duration) {
    if t == nil { return }
    t.mu.Lock()
    defer t.mu.Unlock()
    if !t.enabled { return }
    t.filesDone++
    status := "done"
    if ok {
        status = "done"
    } else {
        status = "fail"
    }
    // 先清掉可能的行尾
    if t.isTTY && t.lastLen > 0 {
        t.printInline("")
    }
    t.println(fmt.Sprintf("[%s] %s | 批次 %d | 总用时 %s",
        status, t.curFileID, t.batchesTotal, formatDur(dur)))
}

// RunFinish: 结束总览。
func (t *Terminal) RunFinish(ok bool, dur time.Duration) {
    if t == nil { return }
    t.mu.Lock()
    defer t.mu.Unlock()
    if !t.enabled { return }
    tag := "ok"
    if !ok {
        tag = "fail"
    }
    t.println(fmt.Sprintf("[%s] 全部完成 | 文件 %d | 总用时 %s", tag, t.filesDone, formatDur(dur)))
}

// 内部输出工具
func (t *Terminal) println(s string) {
    if t == nil || !t.enabled { return }
    if _, err := io.WriteString(t.w, s+"\n"); err != nil {
        // 写失败即禁用
        t.enabled = false
    }
    t.lastLen = 0
}

func (t *Terminal) printInline(s string) {
    if t == nil || !t.enabled { return }
    // 组装：\r + 内容 + 清尾空格
    // 清尾：若新行比旧短，填充空格覆盖
    pad := 0
    if l := visLen(s); t.lastLen > l {
        pad = t.lastLen - l
    }
    var b strings.Builder
    b.WriteByte('\r')
    b.WriteString(s)
    if pad > 0 {
        b.WriteString(strings.Repeat(" ", pad))
    }
    if _, err := io.WriteString(t.w, b.String()); err != nil {
        t.enabled = false
        return
    }
    t.lastLen = visLen(s)
}

// shortenBase: 取基名并按可见宽度截断（尾部省略号）。
func shortenBase(s string, max int) string {
    if max <= 0 { return "" }
    base := filepath.Base(strings.TrimSpace(s))
    if base == "" { return "" }
    if visLen(base) <= max { return base }
    // 预留 1 个字符给省略号
    cut := max - 1
    if cut < 1 { cut = 1 }
    // 简单按 rune 截断
    rs := []rune(base)
    if len(rs) <= cut { return string(rs) }
    return string(rs[:cut]) + "…"
}

func visLen(s string) int { return len([]rune(s)) }

func safe(s string) string {
    // 避免换行等控制字符污染终端
    s = strings.ReplaceAll(s, "\n", " ")
    s = strings.ReplaceAll(s, "\r", " ")
    return s
}

func formatSince(t0 time.Time) string { return formatDur(time.Since(t0)) }

func formatDur(d time.Duration) string {
    if d < time.Second {
        ms := d.Milliseconds()
        if ms <= 0 { ms = 0 }
        return fmt.Sprintf("%dms", ms)
    }
    // 秒，保留 1 位小数
    s := float64(d.Milliseconds()) / 1000.0
    return fmt.Sprintf("%.1fs", s)
}
