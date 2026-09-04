package types

import (
	"context"
)

// ScrapeRequest represents a scrape request
type ScrapeRequest struct {
	Title            string
	MediaType        string
	Season           int
	Episode          *int
	MediaOnlyID      string
	Collection       string
	AlternativeTitle string
	Year             string
}

// ScrapeResult represents a processed torrent result
type ScrapeResult struct {
	Title     string `json:"title"`
	InfoHash  string `json:"infoHash"`
	Link      string
	LinkHash  string
	FileIndex *int     `json:"fileIndex"`
	Seeders   *int     `json:"seeders"`
	Size      int64    `json:"size"`
	Tracker   string   `json:"tracker"`
	Sources   []string `json:"sources"`
}

// TorrentMetadata represents extracted torrent metadata
type TorrentMetadata struct {
	InfoHash     string
	Files        []TorrentFile
	AnnounceList []string
}

// TorrentFile represents a file in a torrent
type TorrentFile struct {
	Name  string
	Index int
	Size  int64
}

// TorrentManager interface
type TorrentManager interface {
	DownloadTorrent(url string) (content []byte, error error)
	ExtractTorrentMetadata(content []byte) (*TorrentMetadata, error)
	ExtractHashFromMagnet(magnetURL string) string
	ExtractTrackersFromMagnet(magnetURL string) []string
	// GetCachedTorrentFiles(hash string) ([]TorrentFile, bool, error)
}

// Scraper is the interface all scrapers implement.
type Scraper interface {
	Name() string
	Id() string
	IsEnabled() bool
	// Scrape performs a query (use ctx to set timeouts/cancellation).
	Scrape(ctx context.Context, request ScrapeRequest, torrentMgr TorrentManager) ([]ScrapeResult, error)
}

var Scrapers []Scraper

type StremioAddon interface {
	Search(ctx context.Context, request ScrapeRequest) []ScrapeResult
}

var Stremio StremioAddon
