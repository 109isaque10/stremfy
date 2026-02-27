package caching

import (
	"context"
	"encoding/gob"
	"os"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"
)

type CacheInstance struct {
	Cache    *ttlcache.Cache[string, any]
	mu       sync.RWMutex
	dirty    bool
	filePath string
}

type cacheData map[string]struct {
	Value any
	TTL   time.Duration
}

var globalCache *CacheInstance

// NewCache creates a new cache instance
func newCache() *CacheInstance {
	c := ttlcache.New[string, any](ttlcache.WithDisableTouchOnHit[string, any]())
	currentDir, _ := os.Getwd()
	filePath := currentDir + "/.cache-snapshot.gob"
	cacheInstance := &CacheInstance{
		Cache:    c,
		mu:       sync.RWMutex{},
		dirty:    false,
		filePath: filePath,
	}

	cacheInstance.loadFromFile() // Load existing cache data from disk

	go c.OnInsertion(func(ctx context.Context, item *ttlcache.Item[string, any]) {
		if item.TTL() == ttlcache.NoTTL {
			cacheInstance.mu.Lock()
			cacheInstance.dirty = true
			cacheInstance.mu.Unlock()
		}
	})

	// Start periodic save every 5 minutes
	go cacheInstance.startPeriodicSave(30 * time.Second)

	// Start periodic cleanup
	go c.Start()

	return cacheInstance
}

func C() *CacheInstance {
	if globalCache == nil {
		globalCache = newCache()
	}
	return globalCache
}

func (c *CacheInstance) startPeriodicSave(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		if c.dirty {
			c.mu.Unlock()
			if err := c.saveToFile(); err != nil {
				zap.L().Error("Failed to save cache", zap.Error(err))
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
func (c *CacheInstance) loadFromFile() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	file, err := os.Open(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, that's okay
			return nil
		}
		return err
	}
	defer file.Close()

	var data cacheData
	if err := gob.NewDecoder(file).Decode(&data); err != nil {
		return err
	}

	for key, item := range data {
		ttl := item.TTL
		c.Cache.Set(key, item.Value, ttl)
	}

	zap.L().Info("✅ Cache loaded from file", zap.Int("items", c.Cache.Len()))

	return nil
}

// saveToFile saves cache data to disk
func (c *CacheInstance) saveToFile() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	zap.L().Debug("💾 Saving cache to file", zap.String("file", c.filePath))

	data := make(cacheData)
	c.Cache.Range(func(item *ttlcache.Item[string, any]) bool {
		ttl := item.TTL()
		if ttl >= 0 {
			zap.L().Debug("⏳ Skipping non-permanent item", zap.String("key", item.Key()))
			return true // Skip non permanent items
		}
		data[item.Key()] = struct {
			Value any
			TTL   time.Duration
		}{item.Value(), ttl}
		return true
	})

	file, err := os.Create(c.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := gob.NewEncoder(file).Encode(data); err != nil {
		return err
	}

	zap.L().Info("✅ Cache saved to file", zap.Int("items", len(data)), zap.String("file", c.filePath))

	return nil
}

func (c *CacheInstance) Flush() error {
	return c.saveToFile()
}
