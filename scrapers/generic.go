package scrapers

import (
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"stremfy/types"
	"strings"
)

// All generic functions are declared here!

// TorrentMetadata represents extracted torrent metadata
type TorrentMetadata struct {
	InfoHash     string
	Files        []TorrentFile
	AnnounceList []string
}

// TorrentFile represents a file in a torrent
type TorrentFile struct {
	Name  string
	Index int
	Size  int64
}

type ScraperManager interface {
	// Add methods as needed
}

func isEpisodePack(title string, season int, episode int) bool {
	titleLower := strings.ToLower(title)

	// Season range patterns with validation
	// Check if the title contains a season range (e.g., "S01-S03", "S01-03")
	seasonRangePatterns := []struct {
		pattern string
		checker func(string, int, int) bool
	}{
		{
			// S01-S03, S1-S3, S01-03, S1-3
			pattern: `s(\d{1,2})[\s\.]*e(\d{1,2})-e?(\d{1,2})[\s\.]*`,
			checker: func(match string, requestedSeason int, requestedEpisode int) bool {
				re := regexp.MustCompile(`s(\d{1,2})[\s\.]*e(\d{1,2})-e?(\d{1,2})[\s\.]*`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 4 {
					season := parseInt(matches[1])
					start := parseInt(matches[2])
					end := parseInt(matches[3])
					// Accept if requested season is within the range
					return !(season == requestedSeason && requestedEpisode >= start && requestedEpisode <= end)
				}
				return true
			},
		},
	}

	// Check season range patterns
	for _, p := range seasonRangePatterns {
		re := regexp.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a range pattern, check if requested season is in range
			if p.checker(titleLower, season, episode) {
				return true // Valid season pack for this request, don't filter
			}
			return false // Invalid season pack, filter it out
		}
	}

	// Specific season pack patterns (e.g., "Season 1 Complete", "S01 Pack")
	specificSeasonPatterns := []struct {
		pattern string
		checker func(string, int, int) bool
	}{
		{
			// S01, S1 with episodes
			pattern: `s(\d{1,2})[\s\.]*e(\d{1,2})[\s\.]*`,
			checker: func(match string, requestedSeason int, requestedEpisode int) bool {
				re := regexp.MustCompile(`s(\d{1,2})[\s\.]*e(\d{1,2})[\s\.]*`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 3 {
					season := parseInt(matches[1])
					episode := parseInt(matches[2])
					return !(season == requestedSeason && episode == requestedEpisode) // Only accept if it's the requested season
				}
				return true
			},
		},
	}

	// Check specific season pack patterns
	for _, p := range specificSeasonPatterns {
		re := regexp.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a specific season pattern, check if it's the right season
			if p.checker(titleLower, season, episode) {
				return true // Valid season pack for this request, don't filter
			}
			return false // Wrong season, filter it out
		}
	}

	return false
}

// isSeasonPack checks if a title indicates a season pack or complete series
// It filters out titles containing season ranges, complete series, or pack indicators
func isSeasonPack(title string, season int) bool {
	titleLower := strings.ToLower(title)

	// Season range patterns with validation
	// Check if the title contains a season range (e.g., "S01-S03", "S01-03")
	seasonRangePatterns := []struct {
		pattern string
		checker func(string, int) bool
	}{
		{
			// S01-S03, S1-S3, S01-03, S1-3
			pattern: `s(\d{1,2})-s?(\d{1,2})`,
			checker: func(match string, requested int) bool {
				re := regexp.MustCompile(`s(\d{1,2})-s?(\d{1,2})`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					// Accept if requested season is within the range
					return requested >= start && requested <= end
				}
				return false
			},
		},
		{
			// Season 1-3, Season 01-03
			pattern: `season\s(\d{1,2})-(\d{1,2})`,
			checker: func(match string, requested int) bool {
				re := regexp.MustCompile(`season\s(\d{1,2})-(\d{1,2})`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					return requested >= start && requested <= end
				}
				return false
			},
		},
		{
			// Temporada 1-3 (Portuguese)
			pattern: `temporada\s(\d{1,2})-(\d{1,2})`,
			checker: func(match string, requested int) bool {
				re := regexp.MustCompile(`temporada\s(\d{1,2})-(\d{1,2})`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					return requested >= start && requested <= end
				}
				return false
			},
		},
		{
			// 1 a 3 Temporada (Portuguese)
			pattern: `(\d{1,2})[ªa]?[.\s-]*a(?:té|te)?[.\s-]*(\d{1,2})[ªa]?[.\s-]*temporada`,
			checker: func(match string, requested int) bool {
				re := regexp.MustCompile(`(\d{1,2})[ªa]?[.\s-]*a(?:té|te)?[.\s-]*(\d{1,2})[ªa]?[.\s-]*temporada`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					return requested >= start && requested <= end
				}
				return false
			},
		},
	}

	// Check season range patterns
	for _, p := range seasonRangePatterns {
		re := regexp.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a range pattern, check if requested season is in range
			if p.checker(titleLower, season) {
				return false // Valid season pack for this request, don't filter
			}
			return true // Invalid season pack, filter it out
		}
	}

	// Specific season pack patterns (e.g., "Season 1 Complete", "S01 Pack")
	specificSeasonPatterns := []struct {
		pattern string
		checker func(string, int) bool
	}{
		{
			// S01, S1 with pack/complete indicators
			pattern: `s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`,
			checker: func(match string, requested int) bool {
				re := regexp.MustCompile(`s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 2 {
					season := parseInt(matches[1])
					return season == requested // Only accept if it's the requested season
				}
				return false
			},
		},
		{
			// Season 1, Season 01 with pack/complete indicators
			pattern: `season\s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`,
			checker: func(match string, requested int) bool {
				re := regexp.MustCompile(`season\s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 2 {
					season := parseInt(matches[1])
					return season == requested
				}
				return false
			},
		},
		{
			// Temporada 1, Temporada 01 (Portuguese)
			pattern: `temporada\s(\d{1,2})[\s\.]*(completo|completa|pack)?`,
			checker: func(match string, requested int) bool {
				re := regexp.MustCompile(`temporada\s(\d{1,2})[\s\.]*(completo|completa|pack)?`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 2 {
					season := parseInt(matches[1])
					return season == requested
				}
				return false
			},
		},
	}

	// Check specific season pack patterns
	for _, p := range specificSeasonPatterns {
		re := regexp.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a specific season pattern, check if it's the right season
			if p.checker(titleLower, season) {
				return false // Valid season pack for this request, don't filter
			}
			return true // Wrong season, filter it out
		}
	}

	return true
}

// Helper function to parse integers from regex matches
func parseInt(s string) int {
	var result int
	fmt.Sscanf(s, "%d", &result)
	return result
}

func parseSize(size string) int64 {
	sizeSplit := strings.Split(size, " ")
	sizeFloat, _ := strconv.ParseFloat(sizeSplit[0], 64)
	sizeInt := int64(0)
	sizeWeight := strings.ToLower(sizeSplit[1])
	switch sizeWeight {
	case "gb":
		sizeInt = int64(sizeFloat * 1073741824)
		break
	case "mb":
		sizeInt = int64(sizeFloat * 1048576)
		break
	case "kb":
		sizeInt = int64(sizeFloat * 1024)
		break
	}
	return sizeInt
}

// normalizeInfoHash handles both normal (40 char) and double-encoded (80 char) hashes
func normalizeInfoHash(hash string) string {
	hash = strings.TrimSpace(hash)

	// Handle double-encoded hash (80 chars)
	if len(hash) == 80 {
		decoded, err := hex.DecodeString(hash)
		if err != nil {
			log.Printf("⚠️ Failed to decode 80-char hash: %v", err)
			return ""
		}
		hash = string(decoded)
	}

	// Validate and normalize
	hash = strings.ToLower(hash)
	if len(hash) != 40 {
		log.Printf("⚠️ Invalid hash length %d (expected 40): %s", len(hash), hash)
		return ""
	}

	return hash
}

// shouldFilterSeriesResult determines if a series result should be filtered out
func shouldFilterSeriesResult(result JackettResult, request types.ScrapeRequest) bool {
	// Check if it's a season pack (we want those for background prefetching)
	if isSeasonPack(result.Title, request.Season) {
		log.Printf("✅ Valid season pack: %s", result.Title)
		return false // Don't filter
	}

	// Check if it's a specific episode pack (filter these out)
	if isEpisodePack(result.Title, request.Season, *request.Episode) {
		log.Printf("🚫 Filtered episode pack: %s", result.Title)
		return true // Filter
	}

	// Check if it's a complete series pack
	if isCompleteSeriesPack(result.Title) {
		log.Printf("✅ Valid complete pack: %s", result.Title)
		return false // Don't filter
	}

	// It's a valid result
	log.Printf("✅ Valid result: %s", result.Title)
	return false
}

// isCompleteSeriesPack checks if title indicates a complete series pack
func isCompleteSeriesPack(title string) bool {
	titleLower := strings.ToLower(title)
	// Complete series indicators
	completeSeriesKeywords := []string{
		"complete series",
		"full series",
		"série completa", // Portuguese
		"serie completa", // Portuguese (alternative spelling)
		"show pack",
		"show.pack",
		"pack completo",    // Portuguese
		"coleção completa", // Portuguese
		"colecao completa", // Portuguese (without accent)
		" - completo",
		" - completa",
		"(completa)",
		"todas as temporadas",
		"todas temporadas",
		"all seasons",
		"pack",
	}

	for _, keyword := range completeSeriesKeywords {
		if strings.Contains(titleLower, keyword) {
			return true
		}
	}

	return false
}
