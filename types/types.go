package types

import (
	"context"
	"time"
)

// ScrapeRequest represents a scrape request
type ScrapeRequest struct {
	Title       string
	MediaType   string
	Season      int
	Episode     *int
	MediaOnlyID string
}

// ScrapeResult represents a processed torrent result
type ScrapeResult struct {
	Title     string   `json:"title"`
	InfoHash  string   `json:"infoHash"`
	FileIndex *int     `json:"fileIndex"`
	Seeders   *int     `json:"seeders"`
	Size      int64    `json:"size"`
	Tracker   string   `json:"tracker"`
	Sources   []string `json:"sources"`
}

// SearchFunc is a function type for searching torrents
type SearchFunc func(ctx context.Context, req ScrapeRequest) ([]ScrapeResult, error)

// Cache interface for cache operations
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, ttl time.Duration)
	SetPermanent(key string, value interface{})
	Delete(key string)
	Clear()
	Size() int
}
