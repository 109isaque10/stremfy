package debrid

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wasilibs/go-re2"
)

var videoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
	".mpg": true, ".mpeg": true, ".m2ts": true, ".ts": true,
	".vob": true, ".ogv": true,
}

type EpisodeInfo struct {
	Hash    string
	Season  int
	Episode int
}

// Episode-specific patterns (must match exact episode number)
var episodePattern = re2.MustCompile(`\b(?:s|season\s?|temporada\s?|t)?0{0,2}(\d{1,2})[-.\s]?(?:[xe.]|episode|ep)0{0,2}(\d{1,3})(?:\D|$)`)
var seasonOnlyPattern = re2.MustCompile(`\b(?:s|season\s?|temporada\s?|t)0{0,2}(\d{1,2})(?:\D|$)`)
var episodeRangePattern = re2.MustCompile(`e0{0,2}\d{1,2}[\s\._-]-[\s\._-]e?0{0,2}\d{1,2}(?:\D|$)`)
var episodeOnlyPattern = re2.MustCompile(`\b(?:[xe.]|episode|ep)0{0,2}(\d{1,3})(?:\D|$)`)

// IsVideoFile checks if a filename is a video file based on extension
func IsVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return videoExtensions[ext]
}

// IsEpisodeFile checks if a filename matches episode patterns
func IsEpisodeFile(hash, filename string) EpisodeInfo {
	actualFilename := strings.ToLower(filepath.Base(filename))
	dirName := filepath.Dir(filename)
	if dirName != "." && dirName != "/" && dirName != "\\" {
		dirName = strings.ToLower(filepath.Base(dirName))
	} else {
		dirName = ""
	}

	// Reject if filename contains episode ranges (e.g., E01-E02, E01-02, E01-02)
	if episodeRangePattern.MatchString(actualFilename) {
		return EpisodeInfo{Hash: hash, Season: -1, Episode: -1}
	}

	// Check if the actual filename matches the full episode pattern (season + episode)
	if matches := episodePattern.FindStringSubmatch(actualFilename); len(matches) >= 3 {
		seasonNum, _ := strconv.Atoi(matches[1])
		episodeNum, _ := strconv.Atoi(matches[2])
		return EpisodeInfo{Hash: hash, Season: seasonNum, Episode: episodeNum}
	}

	// If filename doesn't have season info, check if:
	// 1. Directory name contains the season
	// 2. Filename contains the episode
	if dirName != "" {
		// If season is in directory, check if filename has episode
		if matches := seasonOnlyPattern.FindStringSubmatch(dirName); len(matches) >= 2 {
			seasonNum, _ := strconv.Atoi(matches[1])
			if episodeOnlyPattern.MatchString(actualFilename) {
				if matches := episodeOnlyPattern.FindStringSubmatch(actualFilename); len(matches) >= 2 {
					episodeNum, _ := strconv.Atoi(matches[1])
					return EpisodeInfo{Hash: hash, Season: seasonNum, Episode: episodeNum}
				}
			}
		}
	}

	return EpisodeInfo{Hash: hash, Season: -1, Episode: -1}
}

// IsFileSizeValid checks if file size meets minimum requirements
func IsFileSizeValid(size int64, isSeries bool) bool {
	const minEpisodeSize = 50 * 1024 * 1024 // 50 MB
	const minMovieSize = 500 * 1024 * 1024  // 500 MB

	if isSeries {
		return size >= minEpisodeSize
	}
	return size >= minMovieSize
}
