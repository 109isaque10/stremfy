package scrapers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"stremfy/types"
	"stremfy/utils"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"go.uber.org/zap"
)

const (
	TorrProxyTimeout = 30 * time.Second
)

// TorrProxyResult represents a result from torrProxy API
type TorrProxyResult struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	Description string    `json:"description,omitempty"`
	Size        string    `json:"size,omitempty"`
	PubDate     time.Time `json:"pubdate,omitempty"`
	Seeders     int       `json:"seeders,omitempty"`
	Leechers    int       `json:"leechers,omitempty"`
	InfoHash    string    `json:"infohash,omitempty"`
	TorrentURL  string    `json:"torrent_url,omitempty"`
	Source      string    `json:"source,omitempty"` // indexer name
}

// TorrProxyScraper handles scraping from torrProxy
type TorrProxyScraper struct {
	manager   ScraperManager
	client    *http.Client
	url       string
	cache     types.Cache
	searchTTL time.Duration
	enabled   bool
}

// NewTorrProxyScraper creates a new torrProxy scraper
func NewTorrProxyScraper(manager ScraperManager, url string, cache types.Cache, searchTTL time.Duration, enabled bool) *TorrProxyScraper {
	return &TorrProxyScraper{
		manager: manager,
		client: &http.Client{
			Timeout: TorrProxyTimeout,
		},
		url:       url,
		cache:     cache,
		searchTTL: searchTTL,
		enabled:   enabled,
	}
}

func (t *TorrProxyScraper) IsEnabled() bool {
	return t.enabled
}

// processTorrent processes a single torrent result from torrProxy
func (t *TorrProxyScraper) processTorrent(
	ctx context.Context,
	result TorrProxyResult,
	mediaID string,
	season int,
	torrentMgr TorrentManager,
) ([]types.ScrapeResult, error) {

	var infoHash string
	var sources []string

	// Step 1: Try to get InfoHash from torrProxy result
	if result.InfoHash != "" {
		infoHash = normalizeInfoHash(result.InfoHash)

		if infoHash != "" {
			zap.L().Debug("📌 Using InfoHash from torrProxy",
				zap.String("infoHash", infoHash),
				zap.String("title", result.Title),
				zap.String("source", result.Source),
				zap.String("size", result.Size))

			// torrProxy results already come with extracted infohash
			return t.buildTorrentResults(result, infoHash, sources, mediaID, season), nil
		}
	}

	// Step 2: Check cache for previously extracted hash
	if result.TorrentURL != "" && t.cache != nil {
		if cachedHash, cachedSources := t.getCachedHash(result.TorrentURL); cachedHash != "" {
			zap.L().Debug("📦 Cache hit for hash",
				zap.String("infoHash", cachedHash),
				zap.String("title", result.Title),
				zap.String("source", result.Source))
			return t.buildTorrentResults(result, cachedHash, cachedSources, mediaID, season), nil
		}
	}

	// Step 3: Download torrent file via torrProxy to extract hash and trackers
	// Build the torrProxy download URL first
	if result.TorrentURL != "" && result.Source != "" {
		torrProxyBase := os.Getenv("TORRPROXY_URL")
		if torrProxyBase == "" {
			torrProxyBase = t.url
		}
		if result.TorrentURL != "" {
			if hash, srcs := t.downloadAndExtractHash(ctx, result.TorrentURL, torrentMgr); hash != "" {
				return t.buildTorrentResults(result, hash, srcs, mediaID, season), nil
			}
		}
	}

	// Step 4: Fallback to TorrentURL (magnet) if present
	if result.TorrentURL != "" && strings.HasPrefix(result.TorrentURL, "magnet:") {
		magnetHash := torrentMgr.ExtractHashFromMagnet(result.TorrentURL)
		if magnetHash != "" {
			infoHash = strings.ToLower(magnetHash)
			sources = torrentMgr.ExtractTrackersFromMagnet(result.TorrentURL)
			zap.L().Debug("🧲 Extracted hash from magnet", zap.String("infoHash", infoHash))
			return t.buildTorrentResults(result, infoHash, sources, mediaID, season), nil
		}
	}

	// If still no hash, skip
	if infoHash == "" {
		zap.L().Warn("⏭️ Skipping torrent, no info hash available",
			zap.String("title", result.Title),
			zap.String("source", result.Source))
		return nil, nil
	}

	return t.buildTorrentResults(result, infoHash, sources, mediaID, season), nil
}

// fetchDetailsPageBody fetches a details page using torrProxyScraper's HTTP client
func (t *TorrProxyScraper) fetchDetailsPageBody(ctx context.Context, pageURL string) (string, error) {
	if pageURL == "" {
		return "", fmt.Errorf("empty url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "stremfy/torrproxy-scraper")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// generateCacheKey generates a cache key for a search query
func (t *TorrProxyScraper) generateCacheKey(query string) string {
	hash := sha256.Sum256([]byte(query))
	return fmt.Sprintf("torrproxy_search_%x", hash)
}

// fetchTorrProxyResults fetches results from torrProxy for a given query
func (t *TorrProxyScraper) fetchTorrProxyResults(ctx context.Context, query string) ([]TorrProxyResult, error) {
	// Check cache first if cache is available
	if t.cache != nil {
		cacheKey := t.generateCacheKey(query)
		if cached, found := t.cache.Get(cacheKey); found {
			if results, ok := cached.([]TorrProxyResult); ok {
				zap.L().Debug("📦 Cache hit for torrProxy search", zap.String("query", query))
				return results, nil
			}
		}
	}

	// Build search URL
	params := url.Values{}
	params.Set("q", query)

	apiURL := fmt.Sprintf("%s/search?%s", t.url, params.Encode())

	zap.L().Debug(fmt.Sprintf("🔍 torrProxy search: %s", query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var torrProxyResp []TorrProxyResult
	if err := json.NewDecoder(resp.Body).Decode(&torrProxyResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	zap.L().Debug(fmt.Sprintf("✅ torrProxy returned %d results", len(torrProxyResp)), zap.String("query", query))

	// Cache the results if cache is available
	if t.cache != nil && t.searchTTL > 0 {
		cacheKey := t.generateCacheKey(query)
		t.cache.Set(cacheKey, torrProxyResp, t.searchTTL)
	}

	return torrProxyResp, nil
}

// Scrape performs the scraping operation
func (t *TorrProxyScraper) Scrape(ctx context.Context, request types.ScrapeRequest, torrentMgr TorrentManager) ([]types.ScrapeResult, error) {
	var queries []string
	if request.MediaType == "movie" {
		queries = append(queries, request.Title)
	} else if request.MediaType == "series" && request.Episode != nil {
		queries = append(queries, fmt.Sprintf("%s s%02d", request.Title, request.Season))
		queries = append(queries, fmt.Sprintf("%s complet", request.Title))
		if request.Season != 1 {
			queries = append(queries, fmt.Sprintf("%s s01-", request.Title))
		}
	}

	// Use a wait group to fetch all queries concurrently
	var wg sync.WaitGroup
	resultsChan := make(chan []TorrProxyResult, len(queries))
	errorsChan := make(chan error, len(queries))

	// Fetch results for all queries concurrently
	for _, query := range queries {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			results, err := t.fetchTorrProxyResults(ctx, q)
			if err != nil {
				errorsChan <- err
				return
			}
			resultsChan <- results
		}(query)
	}

	// Wait for all fetches to complete
	go func() {
		wg.Wait()
		close(resultsChan)
		close(errorsChan)
	}()

	// Collect all results
	var allResults []TorrProxyResult
	seen := make(map[string]bool)

	matcher := utils.NewTitleMatcher(85)
	for results := range resultsChan {
		for _, result := range results {
			// Deduplicate by Link field
			if !seen[result.TorrentURL] {
				seen[result.TorrentURL] = true

				// Filter by title match
				if !matcher.Matches(request.Title, result.Title) || !matcher.Matches(strings.ReplaceAll(request.Title, "complet", "pack"), result.Title) {
					zap.L().Debug(fmt.Sprintf("🚫 Title mismatch: expected '%s', got '%s'", request.Title, result.Title))
					continue
				}

				// Filter out season packs when looking for specific episodes
				if request.MediaType == "series" {
					if result.shouldFilterSeriesResult(request) {
						continue
					}
				}

				allResults = append(allResults, result)
			}
		}
	}

	// Log any errors
	for err := range errorsChan {
		zap.L().Error("Error fetching torrProxy results", zap.Error(err))
	}

	// Process all torrents concurrently
	var processingWg sync.WaitGroup
	torrentsChan := make(chan []types.ScrapeResult, len(allResults))

	for _, result := range allResults {
		processingWg.Add(1)
		go func(r TorrProxyResult) {
			defer processingWg.Done()
			torrents, err := t.processTorrent(ctx, r, request.MediaOnlyID, request.Season, torrentMgr)
			if err != nil {
				zap.L().Error("Error processing torrent",
					zap.String("title", r.Title),
					zap.Error(err),
					zap.String("source", r.Source),
					zap.String("infoHash", r.InfoHash))
				return
			}
			if len(torrents) > 0 {
				torrentsChan <- torrents
			}
		}(result)
	}

	// Wait for all processing to complete
	go func() {
		processingWg.Wait()
		close(torrentsChan)
	}()

	// Collect all processed torrents
	var finalTorrents []types.ScrapeResult
	for torrents := range torrentsChan {
		for _, torrent := range torrents {
			if torrent.InfoHash != "" {
				finalTorrents = append(finalTorrents, torrent)
			}
		}
	}

	return finalTorrents, nil
}

// getCachedHash retrieves hash and sources from cache
func (t *TorrProxyScraper) getCachedHash(link string) (hash string, sources []string) {
	cacheKey := fmt.Sprintf("hash_%s", link)
	cached, found := t.cache.Get(cacheKey)
	if !found {
		return "", nil
	}

	hashData, ok := cached.(map[string]interface{})
	if !ok {
		return "", nil
	}

	if h, ok := hashData["hash"].(string); ok {
		hash = h
	}
	if s, ok := hashData["sources"].([]string); ok {
		sources = s
	}

	return hash, sources
}

// buildTorrentResults constructs the final result slice with torrProxy download link
func (t *TorrProxyScraper) buildTorrentResults(
	result TorrProxyResult,
	infoHash string,
	sources []string,
	mediaID string,
	season int,
) []types.ScrapeResult {
	// Build torrProxy download link
	torrProxyBase := os.Getenv("TORRPROXY_BASE")
	if torrProxyBase == "" {
		torrProxyBase = t.url // fallback to scraper URL
	}

	// Parse size from string to int64
	var sizeBytes int64
	if result.Size != "" {
		sizeBytes = parseSizeString(result.Size)
	}

	// Convert seeders to pointer for consistency with Jackett interface
	seedersPtr := &result.Seeders

	torrent := types.ScrapeResult{
		Title:     result.Title,
		InfoHash:  infoHash,
		FileIndex: nil,
		Seeders:   seedersPtr,
		Size:      sizeBytes,
		Tracker:   result.Source, // use Source as Tracker
		Sources:   sources,
	}

	// Add torrProxy download link to sources (first position)
	if result.TorrentURL != "" {
		torrent.Sources = append([]string{result.TorrentURL}, torrent.Sources...)
	}

	return []types.ScrapeResult{torrent}
}

// parseSizeString converts size strings like "1.2 GB", "500 MB" to bytes
func parseSizeString(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}

	// Simple parser - enhance as needed
	var value float64
	var unit string
	fmt.Sscanf(s, "%f %s", &value, &unit)

	multiplier := int64(1)
	switch {
	case strings.HasPrefix(unit, "kb"):
		multiplier = 1024
	case strings.HasPrefix(unit, "mb"):
		multiplier = 1024 * 1024
	case strings.HasPrefix(unit, "gb"):
		multiplier = 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "tb"):
		multiplier = 1024 * 1024 * 1024 * 1024
	}

	return int64(value * float64(multiplier))
}

// downloadAndExtractHash downloads torrent file and extracts hash/trackers
func (t *TorrProxyScraper) downloadAndExtractHash(
	ctx context.Context,
	link string,
	torrentMgr TorrentManager,
) (hash string, sources []string) {
	content, err := torrentMgr.DownloadTorrent(ctx, link)

	if err != nil {
		zap.L().Error("❌ Failed to download torrent", zap.String("link", link), zap.Error(err))
	}

	// Try torrent file first
	if err == nil && content != nil {
		zap.L().Debug("📥 Downloaded torrent file", zap.String("link", link), zap.Int("size", len(content)))
		metadata, extractErr := torrentMgr.ExtractTorrentMetadata(content)
		if extractErr == nil && metadata != nil && metadata.InfoHash != "" {
			hash = strings.ToLower(metadata.InfoHash)
			sources = metadata.AnnounceList
			zap.L().Debug("📥 Extracted hash from torrent file", zap.String("infoHash", hash))
		} else if extractErr != nil {
			zap.L().Error("Error on extracting hash", zap.String("link", link), zap.Error(extractErr))
		}
	}

	// Cache the result if we got a hash
	if hash != "" && t.cache != nil {
		cacheKey := fmt.Sprintf("hash_%s", link)
		t.cache.SetPermanent(cacheKey, map[string]interface{}{
			"hash":    hash,
			"sources": sources,
		})
		zap.L().Debug("💾 Cached hash for future use")
	}

	return hash, sources
}
