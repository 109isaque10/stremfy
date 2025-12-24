package utils

import (
	"crypto/sha1"
	"fmt"
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
	// Find the info dictionary in the bencode data
	infoStart := findInfoDictStart(content)
	if infoStart == -1 {
		return "", fmt.Errorf("info dictionary not found")
	}

	// Extract the info dictionary
	infoDict, err := extractInfoDict(content[infoStart:])
	if err != nil {
		return "", err
	}

	// Calculate SHA1 hash
	hash := sha1.Sum(infoDict)
	return fmt.Sprintf("%x", hash), nil
}

// findInfoDictStart finds the start position of the info dictionary
func findInfoDictStart(content []byte) int {
	// Look for "4:info" in the bencode data
	needle := []byte("4:info")
	for i := 0; i < len(content)-len(needle); i++ {
		if string(content[i:i+len(needle)]) == string(needle) {
			return i + len(needle)
		}
	}
	return -1
}

// extractInfoDict extracts the complete info dictionary
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
