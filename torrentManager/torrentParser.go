package torrentManager

import (
	"bytes"
	"crypto/sha1"
	"fmt"

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
func calculateInfoHash(torrentMap map[string]interface{}) (string, error) {
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
