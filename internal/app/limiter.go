package app

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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

type rateBucket struct {
	tokens   float64
	last     time.Time
	refill   time.Duration
	capacity float64
}

func (b *rateBucket) allow() bool {
	now := time.Now()
	if b.last.IsZero() {
		b.last = now
		b.tokens = b.capacity
	}
	// refill
	elapsed := now.Sub(b.last)
	if b.refill > 0 {
		b.tokens += float64(elapsed) / float64(b.refill)
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
	}
	b.last = now

	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

type UpdateGuard struct {
	mu           sync.Mutex
	seen         map[string]time.Time
	window       time.Duration
	buckets      map[int64]*rateBucket // per chat
	rateRefill   time.Duration
	rateCapacity float64
}

// --- env helpers with дефолтами ---
func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envDurMs(name string, defMs int) time.Duration {
	if v := os.Getenv(name); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return time.Duration(i) * time.Millisecond
		}
	}
	return time.Duration(defMs) * time.Millisecond
}

func NewUpdateGuard() *UpdateGuard {
	g := &UpdateGuard{
		seen:         make(map[string]time.Time),
		window:       envDurMs("DEDUP_WINDOW_MS", 1500),      // 1.5s по умолчанию
		rateRefill:   envDurMs("RATE_LIMIT_REFILL_MS", 300),  // 1 токен каждые 300ms
		rateCapacity: float64(envInt("RATE_LIMIT_BURST", 1)), // «взрыв» = 1 (строже)
		buckets:      make(map[int64]*rateBucket),
	}
	// фоновая чистка старых ключей
	go func() {
		t := time.NewTicker(time.Second * 10)
		for now := range t.C {
			g.mu.Lock()
			for k, ts := range g.seen {
				if now.Sub(ts) > g.window*2 {
					delete(g.seen, k)
				}
			}
			g.mu.Unlock()
		}
	}()
	return g
}

// построение ключа «что именно кликнули/написали»
func dedupKey(u *tgbotapi.Update) (key string, chatID int64, ok bool) {
	switch {
	case u.CallbackQuery != nil:
		cb := u.CallbackQuery
		chatID = cb.From.ID
		msgID := int64(0)
		if cb.Message != nil {
			msgID = int64(cb.Message.MessageID)
		}
		// одинаковый data по тому же сообщению — это дубль
		return fmt.Sprintf("cb:%d:%d:%s", cb.From.ID, msgID, cb.Data), chatID, true

	case u.Message != nil:
		m := u.Message
		chatID = m.Chat.ID
		txt := m.Text
		// текст + id сообщения — защищаемся от double-send клиента
		return fmt.Sprintf("msg:%d:%d:%s", m.From.ID, m.MessageID, txt), chatID, true
	}
	return "", 0, false
}

func (g *UpdateGuard) Allow(u *tgbotapi.Update) bool {
	key, chatID, ok := dedupKey(u)
	if !ok {
		// не знаем тип апдейта — пропускаем
		return true
	}

	now := time.Now()

	g.mu.Lock()
	// --- DEDUP ---
	if ts, hit := g.seen[key]; hit && now.Sub(ts) < g.window {
		g.mu.Unlock()
		// метрика в файле metrics.go
		metrics.TgUpdatesDroppedDedup.Inc()
		return false
	}
	g.seen[key] = now

	// --- RATE LIMIT PER CHAT ---
	b := g.buckets[chatID]
	if b == nil {
		b = &rateBucket{refill: g.rateRefill, capacity: g.rateCapacity}
		g.buckets[chatID] = b
	}
	g.mu.Unlock()

	if !b.allow() {
		metrics.TgUpdatesDroppedRateLimit.Inc()
		return false
	}
	return true
}
