package torrentManager

import (
	"context"
	"fmt"
	"stremfy/debrid"
	"stremfy/types"
)

// TorrentManager wraps TorBox client and provides torrent management functionality
type TorrentManager struct {
	torboxClient *debrid.Client
	mock         *MockTorrentManager
}

// NewTorrentManager creates a new TorrentManager with TorBox integration
func NewTorrentManager(torboxClient *debrid.Client) *TorrentManager {
	m := NewMockTorrentManager()
	return &TorrentManager{
		torboxClient: torboxClient,
		mock:         m,
	}
}

//func (t *TorrentManager) AddTorrent(magnetURL string, seeders *int, tracker, mediaID string, season int) error {
//	return t.mock.AddTorrent(magnetURL, seeders, tracker, mediaID, season)
//}

func (t *TorrentManager) DownloadTorrent(ctx context.Context, url string) ([]byte, error) {
	return t.mock.downloadTorrent(ctx, url)
}

func (t *TorrentManager) ExtractTorrentMetadata(content []byte) (*types.TorrentMetadata, error) {
	return t.mock.extractTorrentMetadata(content)
}

func (t *TorrentManager) ExtractTrackersFromMagnet(magnetURL string) []string {
	return t.mock.extractTrackersFromMagnet(magnetURL)
}

func (t *TorrentManager) ExtractHashFromMagnet(magnetURL string) string {
	return t.mock.extractHashFromMagnet(magnetURL)
}

func (t *TorrentManager) GetCachedTorrentFiles(hash string) ([]types.TorrentFile, bool, error) {
	if t.torboxClient == nil {
		return nil, false, fmt.Errorf("torbox client not initialized")
	}

	// Check if the torrent is cached
	cacheResults, err := t.torboxClient.CheckCacheSingle(hash)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check cache: %w", err)
	}

	if len(cacheResults) == 0 {
		return nil, false, nil
	}

	// Get files from TorBox
	files, _, err := t.torboxClient.GetTorrentFiles(hash)
	if err != nil {
		return nil, true, fmt.Errorf("failed to get torrent files: %w", err)
	}

	// Convert from debrid.CachedFileInfo to scrapers.TorrentFile
	var torrentFiles []types.TorrentFile
	for _, file := range files {
		torrentFiles = append(torrentFiles, types.TorrentFile{
			Name:  file.Name,
			Index: file.Index,
			Size:  file.Size,
		})
	}

	return torrentFiles, true, nil
}
