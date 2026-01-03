package utils

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"stremfy/scrapers"
	"strings"
	"time"

	"github.com/IncSW/go-bencode"
)

type MockTorrentManager struct{}

func (m *MockTorrentManager) AddTorrent(magnetURL string, seeders *int, tracker, mediaID string, season int) error {
	//TODO implement me
	return nil
}

func (m *MockTorrentManager) DownloadTorrent(ctx context.Context, url string) ([]byte, string, string, error) {
	start := time.Now()
	// Try to download torrent file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	log.Printf("Took %dms to download!\n%s", time.Since(start).Milliseconds(), url)

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("failed to download torrent: status %d", resp.StatusCode)
	}

	// Check if it's a magnet link redirect
	if strings.HasPrefix(resp.Request.URL.String(), "magnet:") {
		magnetURL := resp.Request.URL.String()
		hash := extractHashFromMagnet(magnetURL)
		return nil, hash, magnetURL, nil
	}

	// Read torrent file content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}

	return content, "", "", nil
}

func (m *MockTorrentManager) ExtractTorrentMetadata(content []byte) (*scrapers.TorrentMetadata, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("empty content")
	}

	// Unmarshal returns interface{}, so we need to use type assertion
	result, err := bencode.Unmarshal(content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent: %w", err)
	}

	// Type assert to map
	torrentMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid torrent structure")
	}

	// Calculate info hash
	infoHash, err := calculateInfoHash(content)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate info hash: %w", err)
	}

	// Extract trackers
	trackers := extractTrackersFromMap(torrentMap)

	// Extract files from info dictionary
	var files []scrapers.TorrentFile
	if infoDict, ok := torrentMap["info"].(map[string]interface{}); ok {
		files = extractFilesFromInfo(infoDict)
	}

	metadata := &scrapers.TorrentMetadata{
		InfoHash:     infoHash,
		Files:        files,
		AnnounceList: trackers,
	}

	return metadata, nil
}

// extractFilesFromInfo extracts file information from the info dictionary
func extractFilesFromInfo(infoDict map[string]interface{}) []scrapers.TorrentFile {
	var files []scrapers.TorrentFile

	// Check if it's a multi-file torrent
	if filesList, ok := infoDict["files"].([]interface{}); ok {
		// Multi-file torrent
		for i, fileInterface := range filesList {
			if fileMap, ok := fileInterface.(map[string]interface{}); ok {
				length := int64(0)
				if lengthVal, ok := fileMap["length"].(int64); ok {
					length = lengthVal
				} else if lengthVal, ok := fileMap["length"].(int); ok {
					length = int64(lengthVal)
				}

				// Build file path
				var pathParts []string
				if pathList, ok := fileMap["path"].([]interface{}); ok {
					for _, part := range pathList {
						if partStr, ok := part.(string); ok {
							pathParts = append(pathParts, partStr)
						}
					}
				}

				if len(pathParts) > 0 {
					fileName := filepath.Join(pathParts...)
					files = append(files, scrapers.TorrentFile{
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
			files = append(files, scrapers.TorrentFile{
				Name:  name,
				Index: 0,
				Size:  length,
			})
		}
	}

	return files
}

// extractTrackersFromMap extracts trackers from torrent map
func extractTrackersFromMap(torrentMap map[string]interface{}) []string {
	trackerSet := make(map[string]bool)
	var trackers []string

	// Add main announce URL
	if announce, ok := torrentMap["announce"].(string); ok && announce != "" {
		trackerSet[announce] = true
		trackers = append(trackers, announce)
	}

	// Add announce-list URLs
	if announceList, ok := torrentMap["announce-list"].([]interface{}); ok {
		for _, tierInterface := range announceList {
			if tier, ok := tierInterface.([]interface{}); ok {
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

func (m *MockTorrentManager) ExtractTrackersFromMagnet(magnetURL string) []string {
	var trackers []string

	// Extract tracker URLs from magnet link
	parts := strings.Split(magnetURL, "&")
	for _, part := range parts {
		if strings.HasPrefix(part, "tr=") {
			tracker := strings.TrimPrefix(part, "tr=")
			// URL decode
			tracker = strings.ReplaceAll(tracker, "%3A", ":")
			tracker = strings.ReplaceAll(tracker, "%2F", "/")
			trackers = append(trackers, tracker)
		}
	}

	return trackers
}

func (m *MockTorrentManager) GetCachedTorrentFiles(ctx context.Context, hash string) ([]scrapers.TorrentFile, bool, error) {
	// Mock implementation - returns not cached
	// In a real implementation, this would check TorBox cache and return files
	return nil, false, nil
}

func extractHashFromMagnet(magnetURL string) string {
	// Extract info hash from magnet link
	// Format: magnet:?xt=urn:btih: HASH&...
	re := regexp.MustCompile(`xt=urn:btih:([a-fA-F0-9]{40})`)
	matches := re.FindStringSubmatch(magnetURL)
	if len(matches) > 1 {
		return strings.ToLower(matches[1])
	}
	return ""
}
