package debrid

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var videoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
	".mpg": true, ".mpeg": true, ".m2ts": true, ".ts": true,
	".vob": true, ".ogv": true,
}

// IsVideoFile checks if a filename is a video file based on extension
func IsVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return videoExtensions[ext]
}

// IsEpisodeFile checks if a filename matches episode patterns
func IsEpisodeFile(filename string, season, episode int) bool {
	lowerName := strings.ToLower(filename)

	// Split by "/" to separate directory from filename
	parts := strings.Split(lowerName, "/")
	actualFilename := parts[len(parts)-1] // Get the actual filename (last part)

	// Episode-specific patterns (must match exact episode number)
	episodePatterns := []*regexp.Regexp{
		// S01E01, S1E1, S01E001, S001E001
		regexp.MustCompile(fmt.Sprintf(`\bs0*%de0*%d(?:\D|$)`, season, episode)),

		// 1x01, 1x1, 01x01, 001x001
		regexp.MustCompile(fmt.Sprintf(`\b0*%dx0*%d(?:\D|$)`, season, episode)),

		// Episode format with dash:  S01-E01, S1-E1
		regexp.MustCompile(fmt.Sprintf(`\bs0*%d-e0*%d(?:\D|$)`, season, episode)),

		// Episode format with space: S01 E01, S1 E1
		regexp.MustCompile(fmt.Sprintf(`\bs0*%d\s+e0*%d(?:\D|$)`, season, episode)),

		// Episode format: Season 1.01, Season 01.1
		regexp.MustCompile(fmt.Sprintf(`\bseason\s+0*%d[.\s]+0*%d(?:\D|$)`, season, episode)),

		// Dotted format: 1.01, 1.1, 01.01 (season.episode)
		regexp.MustCompile(fmt.Sprintf(`\b0*%d\.0*%d(?:\D|$)`, season, episode)),
	}

	// Episode-only patterns (for when season is in folder)
	episodeOnlyPatterns := []*regexp.Regexp{
		// Episode 01, Episode 1, Ep01, Ep1, E01, E1
		regexp.MustCompile(fmt.Sprintf(`\b(?:episode|ep|e)[\s\._-]*0*%d(?:\D|$)`, episode)),
	}

	// Reject if filename contains episode ranges (e.g., E01-E02, E01-02, E01-02)
	episodeRangePattern := regexp.MustCompile(`e0*\d+[\s\._-]*-[\s\._-]*e?0*\d+`)
	if episodeRangePattern.MatchString(actualFilename) {
		return false
	}

	// Check if the actual filename matches the full episode pattern (season + episode)
	for _, pattern := range episodePatterns {
		if pattern.MatchString(actualFilename) {
			return true
		}
	}

	// If filename doesn't have season info, check if:
	// 1. Directory name contains the season
	// 2. Filename contains the episode
	if len(parts) > 1 {
		dirName := parts[len(parts)-2]

		// Season patterns to check in directory
		seasonPatterns := []*regexp.Regexp{
			regexp.MustCompile(fmt.Sprintf(`\bs0*%d(?:\D|$)`, season)),
			regexp.MustCompile(fmt.Sprintf(`\bseason[\s\._-]*0*%d(?:\D|$)`, season)),
			regexp.MustCompile(fmt.Sprintf(`\btemporada[\s\._-]*0*%d(?:\D|$)`, season)),
		}

		// Check if directory contains season
		seasonInDir := false
		for _, pattern := range seasonPatterns {
			if pattern.MatchString(dirName) {
				seasonInDir = true
				break
			}
		}

		// If season is in directory, check if filename has episode
		if seasonInDir {
			for _, pattern := range episodeOnlyPatterns {
				if pattern.MatchString(actualFilename) {
					return true
				}
			}
		}
	}

	return false
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
