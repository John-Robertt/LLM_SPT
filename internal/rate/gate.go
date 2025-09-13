package rate

import (
	"context"
	"sync"
	"time"

	"llmspt/pkg/contract"
)

// LimitKey: 限流分组键（例如 provider 名称）。
type LimitKey string

// Limits: 每分组的限额配置。0 表示该维度不启用。
type Limits struct {
	RPM             int // requests per minute
	TPM             int // tokens per minute
	MaxTokensPerReq int // 单次请求 token 上限（含输入+预期输出），0 表示不限制
}

// Ask: 一次放行申请。
type Ask struct {
	Key      LimitKey
	Requests int // 默认为 1；必须 >=1
	Tokens   int // 预计 token （>=0）
}

// Gate: 限流闸门（并发安全）。
type Gate interface {
	// Wait: 阻塞直到额度可用或 ctx 取消；违反单请求上限时快速失败。
	Wait(ctx context.Context, a Ask) error
	// Try: 非阻塞尝试；不足时返回 false。
	Try(a Ask) bool
}

// Snapshoter: 可选诊断接口。
type Snapshoter interface {
	Snapshot(key LimitKey) (rpmAvail, tpmAvail int)
}

// NewGate: 从静态配置构造闸门；clk 为空则使用 time.Now。
func NewGate(m map[LimitKey]Limits, clk func() time.Time) Gate {
	if clk == nil {
		clk = time.Now
	}
	g := &gate{clk: clk, m: make(map[LimitKey]*entry, len(m))}
	now := clk()
	for k, lim := range m {
		g.m[k] = newEntry(lim, now)
	}
	return g
}

type gate struct {
	clk func() time.Time
	m   map[LimitKey]*entry
}

type entry struct {
	mu  sync.Mutex
	lim Limits
	req bucket // RPM 维度
	tok bucket // TPM 维度
}

type bucket struct {
	cap   int
	level float64
	rate  float64
	last  time.Time
}

func newEntry(lim Limits, now time.Time) *entry {
	e := &entry{lim: lim}
	if lim.RPM > 0 {
		e.req = newBucket(lim.RPM, now)
	}
	if lim.TPM > 0 {
		e.tok = newBucket(lim.TPM, now)
	}
	return e
}

func newBucket(capacity int, now time.Time) bucket {
	if capacity <= 0 {
		return bucket{}
	}
	return bucket{cap: capacity, level: float64(capacity), rate: float64(capacity) / 60.0, last: now}
}

func (b *bucket) enabled() bool { return b.cap > 0 }

func (b *bucket) refill(now time.Time) {
	if !b.enabled() {
		return
	}
	if now.Before(b.last) {
		// 单调性保护：若时钟回拨，视为无时间流逝
		return
	}
	dt := now.Sub(b.last).Seconds()
	if dt <= 0 {
		return
	}
	b.level += dt * b.rate
	if b.level > float64(b.cap) {
		b.level = float64(b.cap)
	}
	b.last = now
}

func (b *bucket) canTake(n int) bool {
	if !b.enabled() { // 该维度关闭
		return true
	}
	if n <= 0 { // 非法输入在上层校验，这里宽松处理
		return true
	}
	return b.level >= float64(n)
}

func (b *bucket) take(n int) {
	if !b.enabled() || n <= 0 {
		return
	}
	b.level -= float64(n)
	if b.level < 0 {
		b.level = 0
	}
}

// waitSecFor 返回达到可消费 n 还需等待的秒数（向下近似）；上层应取两维度的最大值并做向上取整。
func (b *bucket) waitSecFor(n int) float64 {
	if !b.enabled() || n <= 0 {
		return 0
	}
	deficit := float64(n) - b.level
	if deficit <= 0 {
		return 0
	}
	// 速率为 tokens/sec
	return deficit / b.rate
}

func (g *gate) get(key LimitKey) *entry {
	e := g.m[key]
	if e == nil {
		// 未配置的 key 视为不限额；返回一个禁用两个桶的 entry
		e = newEntry(Limits{}, g.clk())
		g.m[key] = e
	}
	return e
}

func (g *gate) Try(a Ask) bool {
	if a.Requests <= 0 || a.Tokens < 0 {
		return false
	}
	e := g.get(a.Key)
	if e.lim.MaxTokensPerReq > 0 && a.Tokens > e.lim.MaxTokensPerReq {
		return false
	}
	now := g.clk()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.req.refill(now)
	e.tok.refill(now)
	if e.req.canTake(a.Requests) && e.tok.canTake(a.Tokens) {
		e.req.take(a.Requests)
		e.tok.take(a.Tokens)
		return true
	}
	return false
}

func (g *gate) Wait(ctx context.Context, a Ask) error {
	if a.Requests <= 0 || a.Tokens < 0 {
		return contract.ErrInvalidInput
	}
	e := g.get(a.Key)
	if e.lim.MaxTokensPerReq > 0 && a.Tokens > e.lim.MaxTokensPerReq {
		return contract.ErrInvalidInput
	}
	// 最小睡眠粒度，避免忙等
	const minSleep = 10 * time.Millisecond
	for {
		// 快速取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		now := g.clk()
		e.mu.Lock()
		e.req.refill(now)
		e.tok.refill(now)
		canReq := e.req.canTake(a.Requests)
		canTok := e.tok.canTake(a.Tokens)
		if canReq && canTok {
			e.req.take(a.Requests)
			e.tok.take(a.Tokens)
			e.mu.Unlock()
			return nil
		}
		// 计算需要等待的时间（秒）并取最大值
		wr := e.req.waitSecFor(a.Requests)
		wt := e.tok.waitSecFor(a.Tokens)
		e.mu.Unlock()

		waitSec := wr
		if wt > waitSec {
			waitSec = wt
		}
		// 向上取整到 minSleep 的近似倍数
		d := time.Duration(waitSec*float64(time.Second) + float64(minSleep))
		if d < minSleep {
			d = minSleep
		}
		// 分片睡眠以响应 ctx 取消
		if err := sleepCtx(ctx, d); err != nil {
			return err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	// 若 d 很长，分片为最多 200ms 的步长，及时响应取消
	const step = 200 * time.Millisecond
	for d > 0 {
		s := d
		if s > step {
			s = step
		}
		t := time.NewTimer(s)
		select {
		case <-ctx.Done():
			if !t.Stop() {
				<-t.C
			}
			return ctx.Err()
		case <-t.C:
		}
		d -= s
	}
	return nil
}

// Snapshot: 返回当前可用请求/令牌的“向下取整”估值（仅诊断）。
func (g *gate) Snapshot(key LimitKey) (rpmAvail, tpmAvail int) {
	e := g.get(key)
	now := g.clk()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.req.refill(now)
	e.tok.refill(now)
	if e.req.enabled() {
		if e.req.level < 0 {
			rpmAvail = 0
		} else if e.req.level > float64(e.req.cap) {
			rpmAvail = e.req.cap
		} else {
			rpmAvail = int(e.req.level)
		}
	}
	if e.tok.enabled() {
		if e.tok.level < 0 {
			tpmAvail = 0
		} else if e.tok.level > float64(e.tok.cap) {
			tpmAvail = e.tok.cap
		} else {
			tpmAvail = int(e.tok.level)
		}
	}
	return
}

// 接口断言（可选）。
var _ Gate = (*gate)(nil)
var _ Snapshoter = (*gate)(nil)
