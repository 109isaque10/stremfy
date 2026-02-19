package scrapers

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"stremfy/caching"
	"stremfy/types"
	"strings"

	"github.com/wasilibs/go-re2"
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

var seasonEpisodeRangePattern = re2.MustCompile(`s(\d{1,2})[\s\.]*e(\d{1,2})-e?(\d{1,2})[\s\.]*`)
var specificSeasonEpisodePattern = re2.MustCompile(`s(\d{1,2})[\s\.]*e(\d{1,2})[\s\.]*`)
var seasonRangePattern = re2.MustCompile(`(?:s|season\s?|temporada\s?)(\d{1,2})-[ts.]?(\d{1,2})`)
var seasonRangePatternPortuguese = re2.MustCompile(`(\d{1,2})[ªa]?[.\s-]*a(?:té|te)?[.\s-]*(\d{1,2})[ªa]?[.\s-]*temporada`)
var specificSeasonPattern = re2.MustCompile(`(?:s|season\s?|temporada\s?)(\d{1,2})[\s\.]?(complete|pack|completo|completa)?`)

// isEpisodePack checks if a title indicates an episode pack (multiple episodes in the same season)
// It filters out titles containing episode ranges or specific episode indicators that don't match the requested season/episode
// -1 == invalidEpisodePack
// 0 == invalidPack (wrong season/episode)
// 1 == validEpisodePack

func isEpisodePack(hash, title string, season int, episode int) int {
	c := caching.C()
	var cacheKey string
	if c != nil {
		cacheKey = fmt.Sprintf("isEpisodePack:%s:%s:s%de%d", hash, title, season, episode)
		if cached, found := c.Get(cacheKey); found {
			return cached.(int)
		}
	}

	titleLower := strings.ToLower(title)

	// Season range patterns with validation
	// Check if the title contains a season range (e.g., "S01-S03", "S01-03")
	if seasonRangePattern.MatchString(titleLower) {
		matches := seasonRangePattern.FindStringSubmatch(titleLower)
		if len(matches) == 4 {
			matchSeason := parseInt(matches[1])
			start := parseInt(matches[2])
			end := parseInt(matches[3])
			// Accept if requested season is within the range
			if !(matchSeason == season && episode >= start && episode <= end) {
				if c != nil {
					c.SetPermanent(cacheKey, 1)
				}
				return 1
			}
			return 0
		}
		if c != nil {
			c.SetPermanent(cacheKey, -1)
		}
		return -1
	}

	// Specific season pack patterns (e.g., "Season 1 Complete", "S01 Pack")
	if specificSeasonPattern.MatchString(titleLower) {
		matches := specificSeasonPattern.FindStringSubmatch(titleLower)
		if len(matches) == 3 {
			matchSeason := parseInt(matches[1])
			matchEpisode := parseInt(matches[2])
			// Accept if requested season is within the range
			if !(matchSeason == season && matchEpisode == episode) {
				if c != nil {
					c.SetPermanent(cacheKey, 1)
				}
				return 1
			}
			return 0
		}
		if c != nil {
			c.SetPermanent(cacheKey, -1)
		}
		return -1
	}

	if c != nil {
		c.SetPermanent(cacheKey, -1)
	}
	return -1
}

// isSeasonPack checks if a title indicates a season pack or complete series
// It filters out titles containing season ranges, complete series, or pack indicators
// -1 == invalidSeasonPack
// 0 == invalidPack (wrong season)
// 1 == validSeasonPack
func isSeasonPack(hash, title string, season int) int {
	c := caching.C()
	var cacheKey string
	if c != nil {
		cacheKey = fmt.Sprintf("isSeasonPack:%s:%s:s%d", hash, title, season)
		if cached, found := c.Get(cacheKey); found {
			return cached.(int)
		}
	}

	titleLower := strings.ToLower(title)

	// Season range patterns with validation
	// Check if the title contains a season range (e.g., "S01-S03", "S01-03")
	if seasonRangePattern.MatchString(titleLower) {
		matches := seasonRangePattern.FindStringSubmatch(titleLower)
		if len(matches) == 3 {
			start := parseInt(matches[1])
			end := parseInt(matches[2])
			// Accept if requested season is within the range
			if season >= start && season <= end {
				if c != nil {
					c.SetPermanent(cacheKey, 1)
				}
				return 1
			}
			return 0
		}
		if c != nil {
			c.SetPermanent(cacheKey, -1)
		}
		return -1
	}

	if seasonRangePatternPortuguese.MatchString(titleLower) {
		matches := seasonRangePatternPortuguese.FindStringSubmatch(titleLower)
		if len(matches) == 3 {
			start := parseInt(matches[1])
			end := parseInt(matches[2])
			// Accept if requested season is within the range
			if season >= start && season <= end {
				if c != nil {
					c.SetPermanent(cacheKey, 1)
				}
				return 1
			}
			return 0
		}
		if c != nil {
			c.SetPermanent(cacheKey, -1)
		}
		return -1
	}

	// Specific season pack patterns (e.g., "Season 1 Complete", "S01 Pack")
	if specificSeasonPattern.MatchString(titleLower) {
		matches := specificSeasonPattern.FindStringSubmatch(titleLower)
		if len(matches) >= 2 {
			if parseInt(matches[1]) == season {
				if c != nil {
					c.SetPermanent(cacheKey, -1)
				}
				return 1
			}
			return 0
		}
		if c != nil {
			c.SetPermanent(cacheKey, -1)
		}
		return -1
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
	seasonPack := isSeasonPack(infohash, title, request.Season)
	if seasonPack == 1 {
		zap.L().Debug("✅ Valid season pack", zap.String("resultTitle", title), zap.String("title", request.Title), zap.Int("season", request.Season), zap.String("infoHash", infohash))
		return false // Don't filter
	} else if seasonPack == 0 {
		zap.L().Debug("🚫 Invalid Pack (Valid SeasonPack, invalid Season)", zap.String("resultTitle", title), zap.String("title", request.Title), zap.Int("season", request.Season), zap.String("infoHash", infohash))
		return true // Filter
	}

	episodePack := isEpisodePack(infohash, title, request.Season, *request.Episode)
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
