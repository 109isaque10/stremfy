package torrentManager

import (
	"bytes"
	"crypto/sha1"
	"fmt"

	"github.com/IncSW/go-bencode"
)

// Bencode structures for parsing torrent files
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
func calculateInfoHash(content []byte) (string, error) {
	// Check for empty content
	if len(content) == 0 {
		return "", fmt.Errorf("empty content")
	}

	// Unmarshal the torrent file to get the info dictionary
	torrentData, err := bencode.Unmarshal(content)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal torrent: %w", err)
	}

	// Type assert to map
	torrentMap, ok := torrentData.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid torrent structure")
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

// findInfoDictStart finds the start position of the info dictionary
// This is kept for backwards compatibility but should use the proper method above
func findInfoDictStart(content []byte) int {
	// Look for "4:info" in the bencode data
	needle := []byte("4:info")
	idx := bytes.Index(content, needle)
	if idx == -1 {
		return -1
	}
	return idx + len(needle)
}

// extractInfoDict extracts the complete info dictionary
// This is kept for backwards compatibility but should use the proper method above
func extractInfoDict(content []byte) ([]byte, error) {
	if len(content) == 0 || content[0] != 'd' {
		return nil, fmt.Errorf("info dict should start with 'd'")
	}

	depth := 0
	for i := 0; i < len(content); i++ {
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
