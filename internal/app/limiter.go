package app

import (
	"fmt"
	"sync"
	"time"
)

// ---------------- Dedup guard ----------------

type DupeGuard struct {
	mu   sync.Mutex
	seen map[string]int64 // key -> last seen (unix nano)
	ttl  time.Duration
}

func NewDupeGuard(ttl time.Duration) *DupeGuard {
	return &DupeGuard{
		seen: make(map[string]int64),
		ttl:  ttl,
	}
}

// ChatLimiter предотвращает одновременный запуск двух сценариев в одном чате.
type ChatLimiter struct {
	mu   sync.Mutex
	byID map[int64]*sync.Mutex
}

func NewChatLimiter() *ChatLimiter {
	return &ChatLimiter{byID: make(map[int64]*sync.Mutex)}
}

func (l *ChatLimiter) lock(chatID int64) func() {
	l.mu.Lock()
	m, ok := l.byID[chatID]
	if !ok {
		m = &sync.Mutex{}
		l.byID[chatID] = m
	}
	l.mu.Unlock()

	m.Lock()
	return func() { m.Unlock() }
}

// Allow returns false if key was seen recently (within TTL).
func (g *DupeGuard) Allow(key string) bool {
	now := time.Now().UnixNano()
	exp := now - g.ttl.Nanoseconds()

	g.mu.Lock()
	defer g.mu.Unlock()

	if ts, ok := g.seen[key]; ok && ts > exp {
		return false
	}
	g.seen[key] = now

	// Lightweight GC: очистим старьё на проходе
	for k, ts := range g.seen {
		if ts < exp {
			delete(g.seen, k)
		}
	}
	return true
}

func (d *DupeGuard) AllowMessage(chatID int64, messageID int) bool {
	return d.Allow(fmt.Sprintf("m:%d:%d", chatID, messageID))
}

func (d *DupeGuard) AllowCallback(chatID int64, msgID int, data string) bool {
	return d.Allow(fmt.Sprintf("cb:%d:%d:%s", chatID, msgID, data))
}

// ---------------- Per-chat rate limiter ----------------

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*bucket
	rps     float64
	burst   int
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[int64]*bucket),
		rps:     rps,
		burst:   burst,
	}
}

func (rl *RateLimiter) Allow(chatID int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b := rl.buckets[chatID]
	now := time.Now()

	if b == nil {
		b = &bucket{tokens: float64(rl.burst), last: now}
		rl.buckets[chatID] = b
	}

	// Пополняем токены со скоростью rps, не выше burst
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * rl.rps
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.last = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}
