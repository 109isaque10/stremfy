package caching

import (
	"encoding/gob"
	"log"
	"os"
	"sync"
	"time"
)

// Item represents a cached item with an expiration time
type Item struct {
	Value        interface{}
	ExpiresAt    time.Time
	NeverExpires bool
}

// Cache is a generic thread-safe cache with TTL support
type Cache struct {
	mu    sync.RWMutex
	items map[string]*Item
	dirty bool
}

// cacheData is used for serialization (gob can't encode mutexes)
type cacheData struct {
	Items map[string]*Item
}

// NewCache creates a new cache instance
func NewCache() *Cache {
	c := &Cache{
		items: make(map[string]*Item),
	}

	// Try to load existing cache from file
	if err := c.loadFromFile(); err != nil {
		log.Printf("⚠️ Could not load cache from file: %v (starting fresh)", err)
	} else {
		log.Printf("✅ Loaded cache from file: %d entries", len(c.items))
	}

	// Start periodic cleanup
	go c.startCleanup(5 * time.Minute)
	go c.startPeriodicSave(30 * time.Second)

	return c
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists {
		return nil, false
	}

	// Check if item has expired
	if !item.NeverExpires && time.Now().After(item.ExpiresAt) {
		// Item has expired, but don't delete it here (will be cleaned up by cleanup goroutine)
		return nil, false
	}

	return item.Value, true
}

// Set stores a value in the cache with a TTL
func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item := &Item{
		Value:        value,
		ExpiresAt:    time.Now().Add(ttl),
		NeverExpires: false,
	}

	c.items[key] = item

	c.dirty = true
}

// SetPermanent stores a value in the cache that never expires
func (c *Cache) SetPermanent(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item := &Item{
		Value:        value,
		NeverExpires: true,
	}

	c.items[key] = item

	c.dirty = true
}

// Delete removes a value from the cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)

	c.dirty = true
}

// Clear removes all items from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*Item)

	c.dirty = true
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.items)
}

// startCleanup starts a goroutine that periodically removes expired items
func (c *Cache) startCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup removes expired items from the cache
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	count := 0

	for key, item := range c.items {
		if !item.NeverExpires && now.After(item.ExpiresAt) {
			delete(c.items, key)
			count++
		}
	}

	if count > 0 {
		// Log cleanup if needed (can be uncommented)
		log.Printf("🧹 Cleaned up %d expired cache entries", count)
	}

	c.dirty = true
}

// GetStats returns cache statistics
func (c *Cache) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := len(c.items)
	permanent := 0
	expired := 0
	now := time.Now()

	for _, item := range c.items {
		if item.NeverExpires {
			permanent++
		} else if now.After(item.ExpiresAt) {
			expired++
		}
	}

	return map[string]interface{}{
		"total_entries":     total,
		"permanent_entries": permanent,
		"expired_entries":   expired,
		"active_entries":    total - expired,
	}
}

func (c *Cache) startPeriodicSave(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		if c.dirty {
			c.mu.Unlock()
			if err := c.saveToFile(); err != nil {
				log.Printf("⚠️ Failed to save cache: %v", err)
			} else {
				c.mu.Lock()
				c.dirty = false
				c.mu.Unlock()
			}
		} else {
			c.mu.Unlock()
		}
	}
}

// loadFromFile loads cache data from disk
func (c *Cache) loadFromFile() error {
	file, err := os.Open(".cache")
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, that's okay
			return nil
		}
		return err
	}
	defer file.Close()

	var data cacheData
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return err
	}

	c.mu.Lock()
	c.items = data.Items
	c.mu.Unlock()

	return nil
}

// saveToFile saves cache data to disk
func (c *Cache) saveToFile() error {
	c.mu.RLock()
	data := cacheData{
		Items: c.items,
	}
	c.mu.RUnlock()

	file, err := os.Create(".cache")
	if err != nil {
		return err
	}

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(data); err != nil {
		file.Close()
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	return nil
}

func (c *Cache) Flush() error {
	return c.saveToFile()
}
