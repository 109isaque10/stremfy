package torrentManager

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"stremfy/types"
	"strings"
	"time"

	"github.com/IncSW/go-bencode"
	"github.com/wasilibs/go-re2"
	"go.uber.org/zap"
)

type MockTorrentManager struct {
	client *http.Client
}

var magnetHashRe = re2.MustCompile(`xt=urn:btih:([a-fA-F0-9]{40}|[a-zA-Z2-7]{32})`)

func NewMockTorrentManager() *MockTorrentManager {
	return &MockTorrentManager{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

//func (m *MockTorrentManager) AddTorrent(magnetURL string, seeders *int, tracker, mediaID string, season int) error {
//	//TODO implement me
//	return nil
//}

func (m *MockTorrentManager) downloadTorrent(url string) ([]byte, error) {
	start := time.Now()

	// Give each download a individual timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Try to download torrent file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.client.Do(req)
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

// extractInfoDict extracts the complete info dictionary
// This is kept for backwards compatibility but should use the proper method above
func extractInfoDict(content []byte) ([]byte, error) {
	if len(content) == 0 || content[0] != 'd' {
		return nil, fmt.Errorf("info dict should start with 'd'")
	}

	depth := 0
	for i := range len(content) {
		switch content[i] {
		case 'd', 'l':
			depth++
		case 'e':
			depth--
			if depth == 0 {
				return content[:i+1], nil
			}
		}
	}

	return nil, fmt.Errorf("malformed info dictionary")
}

// extractTrackers extracts all tracker URLs from the torrent
func extractTrackers(torrent TorrentFileBencode) []string {
	trackerSet := make(map[string]bool)
	var trackers []string

	// Add main announce URL
	if torrent.Announce != "" {
		if !trackerSet[torrent.Announce] {
			trackerSet[torrent.Announce] = true
			trackers = append(trackers, torrent.Announce)
		}
	}

	// Add announce-list URLs
	for _, tier := range torrent.AnnounceList {
		for _, tracker := range tier {
			if tracker != "" && !trackerSet[tracker] {
				trackerSet[tracker] = true
				trackers = append(trackers, tracker)
			}
		}
	}

	return trackers
}

func (m *MockTorrentManager) extractHashFromMagnet(magnetURL string) string {
	// Extract info hash from magnet link
	// Format: magnet:?xt=urn:btih: HASH&...
	matches := magnetHashRe.FindStringSubmatch(magnetURL)
	if len(matches) > 1 {
		return strings.ToLower(matches[1])
	}
	return ""
}

// extractFilesFromInfo extracts file information from the info dictionary
func extractFilesFromInfo(infoDict map[string]any) []types.TorrentFile {
	var files []types.TorrentFile

	// Check if it's a multi-file torrent
	if filesList, ok := infoDict["files"].([]any); ok {
		// Multi-file torrent
		for i, fileInterface := range filesList {
			if fileMap, ok := fileInterface.(map[string]any); ok {
				length := int64(0)
				if lengthVal, ok := fileMap["length"].(int64); ok {
					length = lengthVal
				} else if lengthVal, ok := fileMap["length"].(int); ok {
					length = int64(lengthVal)
				}

				// Build file path
				var pathParts []string
				if pathList, ok := fileMap["path"].([]any); ok {
					for _, part := range pathList {
						if partStr, ok := part.(string); ok {
							pathParts = append(pathParts, partStr)
						}
					}
				}

				if len(pathParts) > 0 {
					fileName := filepath.Join(pathParts...)
					files = append(files, types.TorrentFile{
						Name:  fileName,
						Index: i,
						Size:  length,
					})
				}
			}
		}
	} else {
		// Single-file torrent
		name := ""
		if nameVal, ok := infoDict["name"].(string); ok {
			name = nameVal
		}

		length := int64(0)
		if lengthVal, ok := infoDict["length"].(int64); ok {
			length = lengthVal
		} else if lengthVal, ok := infoDict["length"].(int); ok {
			length = int64(lengthVal)
		}

		if name != "" {
			files = append(files, types.TorrentFile{
				Name:  name,
				Index: 0,
				Size:  length,
			})
		}
	}

	return files
}

// extractTrackersFromMap extracts trackers from torrent map
func extractTrackersFromMap(torrentMap map[string]any) []string {
	trackerSet := make(map[string]bool)
	var trackers []string

	// Add main announce URL
	if announce, ok := torrentMap["announce"].(string); ok && announce != "" {
		trackerSet[announce] = true
		trackers = append(trackers, announce)
	}

	// Add announce-list URLs
	if announceList, ok := torrentMap["announce-list"].([]any); ok {
		for _, tierInterface := range announceList {
			if tier, ok := tierInterface.([]any); ok {
				for _, trackerInterface := range tier {
					if tracker, ok := trackerInterface.(string); ok && tracker != "" {
						if !trackerSet[tracker] {
							trackerSet[tracker] = true
							trackers = append(trackers, tracker)
						}
					}
				}
			}
		}
	}

	return trackers
}

func (m *MockTorrentManager) extractTrackersFromMagnet(magnetURL string) []string {
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

func (m *MockTorrentManager) extractTorrentMetadata(content []byte) (*types.TorrentMetadata, error) {
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
