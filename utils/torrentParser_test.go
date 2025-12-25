package utils

import (
	"testing"

	"github.com/IncSW/go-bencode"
)

// TestCalculateInfoHash tests the infohash generation
func TestCalculateInfoHash(t *testing.T) {
	// Create a simple torrent structure
	torrent := map[string]interface{}{
		"announce": "http://tracker.example.com:80/announce",
		"info": map[string]interface{}{
			"name":         "test.file.mkv",
			"piece length": int64(262144),
			"pieces":       "12345678901234567890", // 20 bytes (SHA1 hash)
			"length":       int64(1024000),
		},
	}

	// Marshal the torrent
	content, err := bencode.Marshal(torrent)
	if err != nil {
		t.Fatalf("Failed to marshal torrent: %v", err)
	}

	// Calculate infohash
	infoHash, err := calculateInfoHash(content)
	if err != nil {
		t.Fatalf("Failed to calculate infohash: %v", err)
	}

	// Check that we got a valid SHA1 hash (40 hex characters)
	if len(infoHash) != 40 {
		t.Errorf("Expected infohash length 40, got %d", len(infoHash))
	}

	// Check that it's all lowercase hex
	for _, c := range infoHash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Infohash contains invalid character: %c", c)
		}
	}

	// Calculate again to verify consistency
	infoHash2, err := calculateInfoHash(content)
	if err != nil {
		t.Fatalf("Failed to calculate infohash second time: %v", err)
	}

	if infoHash != infoHash2 {
		t.Errorf("Infohash not consistent: %s != %s", infoHash, infoHash2)
	}
}

// TestCalculateInfoHashWithMultipleFiles tests infohash with multi-file torrent
func TestCalculateInfoHashWithMultipleFiles(t *testing.T) {
	// Create a multi-file torrent structure
	torrent := map[string]interface{}{
		"announce": "http://tracker.example.com:80/announce",
		"info": map[string]interface{}{
			"name":         "test.folder",
			"piece length": int64(262144),
			"pieces":       "12345678901234567890",
			"files": []interface{}{
				map[string]interface{}{
					"length": int64(512000),
					"path":   []interface{}{"file1.mkv"},
				},
				map[string]interface{}{
					"length": int64(512000),
					"path":   []interface{}{"file2.mkv"},
				},
			},
		},
	}

	// Marshal the torrent
	content, err := bencode.Marshal(torrent)
	if err != nil {
		t.Fatalf("Failed to marshal torrent: %v", err)
	}

	// Calculate infohash
	infoHash, err := calculateInfoHash(content)
	if err != nil {
		t.Fatalf("Failed to calculate infohash: %v", err)
	}

	// Check that we got a valid SHA1 hash
	if len(infoHash) != 40 {
		t.Errorf("Expected infohash length 40, got %d", len(infoHash))
	}
}

// TestCalculateInfoHashEmptyContent tests error handling
func TestCalculateInfoHashEmptyContent(t *testing.T) {
	_, err := calculateInfoHash([]byte{})
	if err == nil {
		t.Error("Expected error for empty content, got nil")
	}
}

// TestCalculateInfoHashInvalidBencode tests error handling for invalid bencode
func TestCalculateInfoHashInvalidBencode(t *testing.T) {
	_, err := calculateInfoHash([]byte("invalid bencode"))
	if err == nil {
		t.Error("Expected error for invalid bencode, got nil")
	}
}

// TestCalculateInfoHashMissingInfo tests error handling when info dict is missing
func TestCalculateInfoHashMissingInfo(t *testing.T) {
	torrent := map[string]interface{}{
		"announce": "http://tracker.example.com:80/announce",
	}

	content, err := bencode.Marshal(torrent)
	if err != nil {
		t.Fatalf("Failed to marshal torrent: %v", err)
	}

	_, err = calculateInfoHash(content)
	if err == nil {
		t.Error("Expected error for missing info dict, got nil")
	}
}
