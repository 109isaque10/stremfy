package utils

import (
	"context"
	"fmt"
	"stremfy/debrid"
	"stremfy/scrapers"
)

// TorrentManager wraps TorBox client and provides torrent management functionality
type TorrentManager struct {
	torboxClient *debrid.Client
	mock         *MockTorrentManager
}

// NewTorrentManager creates a new TorrentManager with TorBox integration
func NewTorrentManager(torboxClient *debrid.Client) *TorrentManager {
	return &TorrentManager{
		torboxClient: torboxClient,
		mock:         &MockTorrentManager{},
	}
}

func (t *TorrentManager) AddTorrent(magnetURL string, seeders *int, tracker, mediaID string, season int) error {
	return t.mock.AddTorrent(magnetURL, seeders, tracker, mediaID, season)
}

func (t *TorrentManager) DownloadTorrent(ctx context.Context, url string) ([]byte, string, string, error) {
	return t.mock.DownloadTorrent(ctx, url)
}

func (t *TorrentManager) ExtractTorrentMetadata(content []byte) (*scrapers.TorrentMetadata, error) {
	return t.mock.ExtractTorrentMetadata(content)
}

func (t *TorrentManager) ExtractTrackersFromMagnet(magnetURL string) []string {
	return t.mock.ExtractTrackersFromMagnet(magnetURL)
}

func (t *TorrentManager) GetCachedTorrentFiles(ctx context.Context, hash string) ([]scrapers.TorrentFile, bool, error) {
	if t.torboxClient == nil {
		return nil, false, nil
	}

	// Check if the torrent is cached
	cacheResults, err := t.torboxClient.CheckCacheSingle(hash)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check cache: %w", err)
	}

	if len(cacheResults) == 0 {
		return nil, false, nil
	}

	cacheResult := cacheResults[0]
	if !cacheResult.Cached {
		return nil, false, nil
	}

	// Get files from TorBox
	files, _, err := t.torboxClient.GetTorrentFiles(hash)
	if err != nil {
		return nil, true, fmt.Errorf("failed to get torrent files: %w", err)
	}

	// Convert from debrid.CachedFileInfo to scrapers.TorrentFile
	var torrentFiles []scrapers.TorrentFile
	for _, file := range files {
		torrentFiles = append(torrentFiles, scrapers.TorrentFile{
			Name:  file.Name,
			Index: file.Index,
			Size:  file.Size,
		})
	}

	return torrentFiles, true, nil
}
