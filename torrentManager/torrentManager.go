package torrentManager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"stremfy/debrid"
	"stremfy/types"
	"strings"
	"time"

	"github.com/IncSW/go-bencode"
	"github.com/wasilibs/go-re2"
	"go.uber.org/zap"
)

var magnetHashRe = re2.MustCompile(`xt=urn:btih:([a-fA-F0-9]{40}|[a-zA-Z2-7]{32})`)

// TorrentManager wraps TorBox client and provides torrent management functionality
type TorrentManager struct {
	torboxClient *debrid.Client
	client       *http.Client
}

// NewTorrentManager creates a new TorrentManager with TorBox integration
func NewTorrentManager(torboxClient *debrid.Client) *TorrentManager {
	return &TorrentManager{
		torboxClient: torboxClient,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *TorrentManager) DownloadTorrent(url string) ([]byte, error) {
	start := time.Now()

	// Give each download a individual timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Try to download torrent file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	zap.L().Debug(fmt.Sprintf("Took %dms to download!", time.Since(start).Milliseconds()))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download torrent: status %d", resp.StatusCode)
	}

	// Read torrent file content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func (t *TorrentManager) ExtractTorrentMetadata(content []byte) (*types.TorrentMetadata, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("empty content")
	}

	// Unmarshal returns any, so we need to use type assertion
	result, err := bencode.Unmarshal(content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent: %w", err)
	}

	// Type assert to map
	torrentMap, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid torrent structure")
	}

	// Calculate info hash
	infoHash, err := calculateInfoHash(torrentMap)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate info hash: %w", err)
	}

	// Extract trackers
	trackers := extractTrackersFromMap(torrentMap)

	// Extract files from info dictionary
	var files []types.TorrentFile
	if infoDict, ok := torrentMap["info"].(map[string]any); ok {
		files = extractFilesFromInfo(infoDict)
	}

	metadata := &types.TorrentMetadata{
		InfoHash:     infoHash,
		Files:        files,
		AnnounceList: trackers,
	}

	return metadata, nil
}

func (t *TorrentManager) ExtractTrackersFromMagnet(magnetURL string) []string {
	var trackers []string

	// Extract tracker URLs from magnet link
	parts := strings.SplitSeq(magnetURL, "&")
	for part := range parts {
		if tracker, found := strings.CutPrefix(part, "tr="); found {
			// URL decode
			tracker = strings.ReplaceAll(tracker, "%3A", ":")
			tracker = strings.ReplaceAll(tracker, "%2F", "/")
			trackers = append(trackers, tracker)
		}
	}

	return trackers
}

func (t *TorrentManager) ExtractHashFromMagnet(magnetURL string) string {
	// Extract info hash from magnet link
	// Format: magnet:?xt=urn:btih: HASH&...
	matches := magnetHashRe.FindStringSubmatch(magnetURL)
	if len(matches) > 1 {
		return strings.ToLower(matches[1])
	}
	return ""
}

// func (t *TorrentManager) GetCachedTorrentFiles(hash string) ([]types.TorrentFile, bool, error) {
// 	if t.torboxClient == nil {
// 		return nil, false, fmt.Errorf("torbox client not initialized")
// 	}

// 	// Check if the torrent is cached
// 	cacheResults, err := t.torboxClient.CheckCacheSingle(hash)
// 	if err != nil {
// 		return nil, false, fmt.Errorf("failed to check cache: %w", err)
// 	}

// 	if len(cacheResults) == 0 {
// 		return nil, false, nil
// 	}

// 	// Get files from TorBox
// 	files, _, err := t.torboxClient.GetTorrentFiles(hash)
// 	if err != nil {
// 		return nil, true, fmt.Errorf("failed to get torrent files: %w", err)
// 	}

// 	// Convert from debrid.CachedFileInfo to scrapers.TorrentFile
// 	var torrentFiles []types.TorrentFile
// 	for _, file := range files {
// 		torrentFiles = append(torrentFiles, types.TorrentFile{
// 			Name:  file.Name,
// 			Index: file.Index,
// 			Size:  file.Size,
// 		})
// 	}

// 	return torrentFiles, true, nil
// }
