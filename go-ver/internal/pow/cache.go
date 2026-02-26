package pow

import (
	"sync"
	"time"
)

type entry struct {
	Val      string
	ExpireAt int64
}

type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
}

func NewCache() *Cache { return &Cache{entries: map[string]entry{}} }

func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if e.ExpireAt > 0 && time.Now().Unix() >= e.ExpireAt {
		delete(c.entries, key)
		return "", false
	}
	return e.Val, true
}

func (c *Cache) Set(key, val string, expireAt int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if expireAt > 0 && time.Now().Unix() >= expireAt {
		return
	}
	c.entries[key] = entry{Val: val, ExpireAt: expireAt - 1}
}
