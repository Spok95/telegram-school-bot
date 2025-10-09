package ctxutil

import (
	"context"
	"time"
)

// приватные ключи, чтобы исключить коллизии
type key int

const (
	keyChatID key = iota
	keyUserID
	keyOpName
)

// WithChatID /ChatID — прокидываем chatID в контекст
func WithChatID(ctx context.Context, chatID int64) context.Context {
	return context.WithValue(ctx, keyChatID, chatID)
}

func ChatID(ctx context.Context) (int64, bool) {
	v := ctx.Value(keyChatID)
	if v == nil {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

// WithUserID /UserID — прокидываем внутренний userID (если есть)
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, keyUserID, userID)
}

func UserID(ctx context.Context) (int64, bool) {
	v := ctx.Value(keyUserID)
	if v == nil {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

// WithOp /Op — имя операции (для логов/трейса)
func WithOp(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, keyOpName, name)
}

func Op(ctx context.Context) (string, bool) {
	v := ctx.Value(keyOpName)
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// Таймауты: общий и для БД.
// Пока константы; при желании позже сделаем из ENV/конфига.
var (
	DefaultDBTimeout = 5 * time.Second
)

// WithTimeout — удобная обёртка над context.WithTimeout.
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		// на всякий случай: если d<=0 — без таймаута
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, d)
}

// WithDBTimeout — стандартный таймаут для БД.
func WithDBTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if dl, ok := parent.Deadline(); ok {
		// если у родителя осталось меньше defaultDBTimeout — берем остаток
		remain := time.Until(dl)
		if remain < DefaultDBTimeout {
			return context.WithTimeout(parent, remain)
		}
	}
	return context.WithTimeout(parent, DefaultDBTimeout)
}
