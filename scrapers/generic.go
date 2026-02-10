package scrapers

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"stremfy/types"
	"strings"

	"github.com/coregx/coregex"
	"go.uber.org/zap"
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

// TorrentManager interface
type TorrentManager interface {
	// DownloadTorrent AddTorrent(magnetURL string, seeders *int, tracker, mediaID string, season int) error
	DownloadTorrent(ctx context.Context, url string) (content []byte, error error)
	ExtractTorrentMetadata(content []byte) (*TorrentMetadata, error)
	ExtractHashFromMagnet(magnetURL string) string
	ExtractTrackersFromMagnet(magnetURL string) []string
	GetCachedTorrentFiles(hash string) ([]TorrentFile, bool, error)
}

type ScraperManager interface {
	// Add methods as needed
}

func isEpisodePack(title string, season int, episode int) int {
	titleLower := strings.ToLower(title)

	// Season range patterns with validation
	// Check if the title contains a season range (e.g., "S01-S03", "S01-03")
	seasonRangePatterns := []struct {
		pattern string
		checker func(string, int, int) int
	}{
		{
			// S01-S03, S1-S3, S01-03, S1-3
			pattern: `s(\d{1,2})[\s\.]*e(\d{1,2})-e?(\d{1,2})[\s\.]*`,
			checker: func(match string, requestedSeason int, requestedEpisode int) int {
				re := coregex.MustCompile(`s(\d{1,2})[\s\.]*e(\d{1,2})-e?(\d{1,2})[\s\.]*`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 4 {
					season := parseInt(matches[1])
					start := parseInt(matches[2])
					end := parseInt(matches[3])
					// Accept if requested season is within the range
					if !(season == requestedSeason && requestedEpisode >= start && requestedEpisode <= end) {
						return 1
					}
					return 0
				}
				return -1
			},
		},
	}

	// Check season range patterns
	for _, p := range seasonRangePatterns {
		re := coregex.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a range pattern, check if requested season is in range
			return p.checker(titleLower, season, episode)
		}
	}

	// Specific season pack patterns (e.g., "Season 1 Complete", "S01 Pack")
	specificSeasonPatterns := []struct {
		pattern string
		checker func(string, int, int) int
	}{
		{
			// S01, S1 with episodes
			pattern: `s(\d{1,2})[\s\.]*e(\d{1,2})[\s\.]*`,
			checker: func(match string, requestedSeason int, requestedEpisode int) int {
				re := coregex.MustCompile(`s(\d{1,2})[\s\.]*e(\d{1,2})[\s\.]*`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 3 {
					season := parseInt(matches[1])
					episode := parseInt(matches[2])
					if !(season == requestedSeason && episode == requestedEpisode) {
						return 1
					}
					return 0
				}
				return -1
			},
		},
	}

	// Check specific season pack patterns
	for _, p := range specificSeasonPatterns {
		re := coregex.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a specific season pattern, check if it's the right season
			return p.checker(titleLower, season, episode)
		}
	}
	return -1
}

// isSeasonPack checks if a title indicates a season pack or complete series
// It filters out titles containing season ranges, complete series, or pack indicators
// -1 == invalidSeasonPack
// 0 == invalidPack (wrong season)
// 1 == validSeasonPack
func isSeasonPack(title string, season int) int {
	titleLower := strings.ToLower(title)

	// Season range patterns with validation
	// Check if the title contains a season range (e.g., "S01-S03", "S01-03")
	seasonRangePatterns := []struct {
		pattern string
		checker func(string, int) int
	}{
		{
			// S01-S03, S1-S3, S01-03, S1-3
			pattern: `s(\d{1,2})-s?(\d{1,2})`,
			checker: func(match string, requested int) int {
				re := coregex.MustCompile(`s(\d{1,2})-s?(\d{1,2})`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					// Accept if requested season is within the range
					if requested >= start && requested <= end {
						return 1
					}
					return 0
				}
				return -1
			},
		},
		{
			// Season 1-3, Season 01-03
			pattern: `season\s(\d{1,2})-(\d{1,2})`,
			checker: func(match string, requested int) int {
				re := coregex.MustCompile(`season\s(\d{1,2})-(\d{1,2})`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					if requested >= start && requested <= end {
						return 1
					}
					return 0
				}
				return -1
			},
		},
		{
			// Temporada 1-3 (Portuguese)
			pattern: `temporada\s(\d{1,2})-(\d{1,2})`,
			checker: func(match string, requested int) int {
				re := coregex.MustCompile(`temporada\s(\d{1,2})-(\d{1,2})`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					if requested >= start && requested <= end {
						return 1
					}
					return 0
				}
				return -1
			},
		},
		{
			// 1 a 3 Temporada (Portuguese)
			pattern: `(\d{1,2})[ªa]?[.\s-]*a(?:té|te)?[.\s-]*(\d{1,2})[ªa]?[.\s-]*temporada`,
			checker: func(match string, requested int) int {
				re := coregex.MustCompile(`(\d{1,2})[ªa]?[.\s-]*a(?:té|te)?[.\s-]*(\d{1,2})[ªa]?[.\s-]*temporada`)
				matches := re.FindStringSubmatch(match)
				if len(matches) == 3 {
					start := parseInt(matches[1])
					end := parseInt(matches[2])
					if requested >= start && requested <= end {
						return 1
					}
					return 0
				}
				return -1
			},
		},
	}

	// Check season range patterns
	for _, p := range seasonRangePatterns {
		re := coregex.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a range pattern, check if requested season is in range
			return p.checker(titleLower, season)
		}
	}

	// Specific season pack patterns (e.g., "Season 1 Complete", "S01 Pack")
	specificSeasonPatterns := []struct {
		pattern string
		checker func(string, int) int
	}{
		{
			// S01, S1 with pack/complete indicators
			pattern: `s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`,
			checker: func(match string, requested int) int {
				re := coregex.MustCompile(`s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 2 {
					// Only accept if it's the requested season
					if parseInt(matches[1]) == requested {
						return 1
					}
					return 0
				}
				return -1
			},
		},
		{
			// Season 1, Season 01 with pack/complete indicators
			pattern: `season\s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`,
			checker: func(match string, requested int) int {
				re := coregex.MustCompile(`season\s(\d{1,2})[\s\.]*(complete|pack|completo|completa)?`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 2 {
					if parseInt(matches[1]) == requested {
						return 1
					}
					return 0
				}
				return -1
			},
		},
		{
			// Temporada 1, Temporada 01 (Portuguese)
			pattern: `temporada\s(\d{1,2})[\s\.]*(completo|completa|pack)?`,
			checker: func(match string, requested int) int {
				re := coregex.MustCompile(`temporada\s(\d{1,2})[\s\.]*(completo|completa|pack)?`)
				matches := re.FindStringSubmatch(match)
				if len(matches) >= 2 {
					if parseInt(matches[1]) == requested {
						return 1
					}
					return 0
				}
				return -1
			},
		},
	}

	// Check specific season pack patterns
	for _, p := range specificSeasonPatterns {
		re := coregex.MustCompile(p.pattern)
		if re.MatchString(titleLower) {
			// If it matches a specific season pattern, check if it's the right season
			return p.checker(titleLower, season)
		}
	}

	return -1
}

// Helper function to parse integers from regex matches
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
			zap.L().Error("Failed to decode 80-char hash", zap.String("hash", hash), zap.Error(err))
			return ""
		}
		hash = string(decoded)
	}

	// Validate and normalize
	hash = strings.ToLower(hash)
	if len(hash) != 40 {
		zap.L().Error(fmt.Sprintf("Invalid hash length %d (expected 40)", len(hash)), zap.String("hash", hash))
		return ""
	}

	return hash
}

func (j JackettResult) shouldFilterSeriesResult(request types.ScrapeRequest) bool {
	return shouldFilterSeriesResult(j.Title, j.InfoHash, request)
}

func (t TorrProxyResult) shouldFilterSeriesResult(request types.ScrapeRequest) bool {
	return shouldFilterSeriesResult(t.Title, t.InfoHash, request)
}

// shouldFilterSeriesResult determines if a series result should be filtered out
func shouldFilterSeriesResult(title, infohash string, request types.ScrapeRequest) bool {
	// Check if it's a season pack (we want those for background prefetching)
	seasonPack := isSeasonPack(title, request.Season)
	if seasonPack == 1 {
		zap.L().Debug("✅ Valid season pack", zap.String("resultTitle", title), zap.String("title", request.Title), zap.Int("season", request.Season), zap.String("infoHash", infohash))
		return false // Don't filter
	} else if seasonPack == 0 {
		zap.L().Debug("🚫 Invalid Pack (Valid SeasonPack, invalid Season)", zap.String("resultTitle", title), zap.String("title", request.Title), zap.Int("season", request.Season), zap.String("infoHash", infohash))
		return true // Filter
	}

	episodePack := isEpisodePack(title, request.Season, *request.Episode)
	if episodePack == 1 {
		zap.L().Debug("✅ Valid episode pack", zap.String("resultTitle", title), zap.String("title", request.Title), zap.Int("season", request.Season),
			zap.Intp("episode", request.Episode), zap.String("infoHash", infohash))
		return false // Don't filter
	} else if episodePack == 0 {
		zap.L().Debug("🚫 Filtered episode pack", zap.String("resultTitle", title), zap.String("title", request.Title), zap.Int("season", request.Season),
			zap.Intp("episode", request.Episode), zap.String("infoHash", infohash))
		return true // Filter
	}
	// Check if it's a specific episode pack (filter these out)

	// Check if it's a complete series pack
	if isCompleteSeriesPack(title) {
		zap.L().Debug("✅ Valid complete pack", zap.String("resultTitle", title), zap.String("title", request.Title), zap.String("infoHash", infohash))
		return false // Don't filter
	}

	// It's a valid result
	zap.L().Debug("✅ Valid result", zap.String("resultTitle", title), zap.String("title", request.Title), zap.String("infoHash", infohash))
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

func parseInt(s string) int {
	var result int
	fmt.Sscanf(s, "%d", &result)
	return result
}
