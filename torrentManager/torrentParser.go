package torrentManager

import (
	"crypto/sha1"
	"fmt"
	"path/filepath"
	"stremfy/types"

	"github.com/IncSW/go-bencode"
)

// TorrentFileBencode structures for parsing torrent files
type TorrentFileBencode struct {
	Announce     string             `bencode:"announce"`
	AnnounceList [][]string         `bencode:"announce-list"`
	Comment      string             `bencode:"comment"`
	CreatedBy    string             `bencode:"created by"`
	CreationDate int64              `bencode:"creation date"`
	Info         TorrentInfoBencode `bencode:"info"`
}

type TorrentInfoBencode struct {
	Name        string                   `bencode:"name"`
	PieceLength int64                    `bencode:"piece length"`
	Pieces      string                   `bencode:"pieces"`
	Private     int64                    `bencode:"private"`
	Length      int64                    `bencode:"length"` // Single file mode
	Files       []TorrentFileInfoBencode `bencode:"files"`  // Multi file mode
}

type TorrentFileInfoBencode struct {
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"`
}

// calculateInfoHash calculates the SHA1 hash of the info dictionary
func calculateInfoHash(torrentMap map[string]any) (string, error) {
	// Check for empty content
	if len(torrentMap) == 0 {
		return "", fmt.Errorf("empty content")
	}

	// Get the info dictionary
	infoDict, ok := torrentMap["info"]
	if !ok {
		return "", fmt.Errorf("info dictionary not found")
	}

	// Marshal the info dictionary back to bencode
	infoBencoded, err := bencode.Marshal(infoDict)
	if err != nil {
		return "", fmt.Errorf("failed to marshal info dict: %w", err)
	}

	// Calculate SHA1 hash
	hash := sha1.Sum(infoBencoded)
	return fmt.Sprintf("%x", hash), nil
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
