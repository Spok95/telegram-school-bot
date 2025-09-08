package app

import "sync"

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
