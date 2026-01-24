package scrapers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"stremfy/types"
	"strings"
	"sync"
	"time"
)

const (
	IndexerTimeout = 30 * time.Second
)

// JackettResult represents a result from Jackett API
type JackettResult struct {
	Title     string `json:"Title"`
	Link      string `json:"Link"`
	InfoHash  string `json:"InfoHash"`
	MagnetUri string `json:"MagnetUri"`
	Seeders   *int   `json:"Seeders"`
	Size      int64  `json:"Size"`
	Tracker   string `json:"Tracker"`
	Details   string `json:"Details"`
	Guid      string `json:"Guid"`
}

// JackettResponse represents the API response
type JackettResponse struct {
	Results []JackettResult `json:"Results"`
}

// JackettScraper handles scraping from Jackett
type JackettScraper struct {
	manager   ScraperManager
	client    *http.Client
	url       string
	apiKey    string
	cache     types.Cache
	searchTTL time.Duration
}

// TorrentManager interface
type TorrentManager interface {
	AddTorrent(magnetURL string, seeders *int, tracker, mediaID string, season int) error
	DownloadTorrent(ctx context.Context, url string) (content []byte, magnetHash string, magnetURL string, error error)
	ExtractTorrentMetadata(content []byte) (*TorrentMetadata, error)
	ExtractTrackersFromMagnet(magnetURL string) []string
	GetCachedTorrentFiles(hash string) ([]TorrentFile, bool, error)
}

// NewJackettScraper creates a new Jackett scraper
func NewJackettScraper(manager ScraperManager, url, apiKey string, cache types.Cache, searchTTL time.Duration) *JackettScraper {
	return &JackettScraper{
		manager: manager,
		client: &http.Client{
			Timeout: IndexerTimeout,
		},
		url:       url,
		apiKey:    apiKey,
		cache:     cache,
		searchTTL: searchTTL,
	}
}

// processTorrent processes a single torrent result
func (j *JackettScraper) processTorrent(
	ctx context.Context,
	result JackettResult,
	mediaID string,
	season int,
	torrentMgr TorrentManager,
) ([]types.ScrapeResult, error) {

	// Get the info hash first
	var infoHash string
	var sources []string

	// Step 1: Try to get InfoHash from Jackett result
	if result.InfoHash != "" {
		infoHash = normalizeInfoHash(result.InfoHash)

		if infoHash != "" {
			log.Printf("📌 Using InfoHash from Jackett: %s", infoHash)

			// Extract trackers from MagnetUri if available
			if result.MagnetUri != "" {
				sources = torrentMgr.ExtractTrackersFromMagnet(result.MagnetUri)
			}

			// Early return - we have everything we need
			return j.buildTorrentResults(result, infoHash, sources, torrentMgr, mediaID, season), nil
		}
	}

	// Step 2: Check cache for previously downloaded hash
	if result.Link != "" && j.cache != nil {
		if cachedHash, cachedSources := j.getCachedHash(result.Link); cachedHash != "" {
			log.Printf("📦 Cache hit for hash: %s", cachedHash)
			return j.buildTorrentResults(result, cachedHash, cachedSources, torrentMgr, mediaID, season), nil
		}
	}

	// Step 3: Download torrent file to extract hash and trackers
	if result.Link != "" {
		if hash, srcs := j.downloadAndExtractHash(ctx, result.Link, torrentMgr); hash != "" {
			return j.buildTorrentResults(result, hash, srcs, torrentMgr, mediaID, season), nil
		}
	}

	// If we don't have an info hash, we can't proceed
	fmt.Printf("⏭️  Skipping torrent %s: no info hash available\n", result.Title)
	return nil, nil
}

// generateCacheKey generates a cache key for a search query
func (j *JackettScraper) generateCacheKey(query string) string {
	hash := sha256.Sum256([]byte(query))
	return fmt.Sprintf("jackett_search_%x", hash)
}

// fetchJackettResults fetches results from Jackett for a given query
func (j *JackettScraper) fetchJackettResults(ctx context.Context, query string) ([]JackettResult, error) {
	// Check cache first if cache is available
	if j.cache != nil {
		cacheKey := j.generateCacheKey(query)
		if cached, found := j.cache.Get(cacheKey); found {
			if results, ok := cached.([]JackettResult); ok {
				fmt.Printf("📦 Cache hit for Jackett search: %s\n", query)
				return results, nil
			}
		}
	}

	// Build URL with 'all' indexer
	params := url.Values{}
	params.Set("apikey", j.apiKey)
	params.Set("Query", query)

	apiURL := fmt.Sprintf("%s/api/v2.0/indexers/all/results?%s", j.url, params.Encode())

	fmt.Printf("🔍 Jackett search: %s\n", query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var jackettResp JackettResponse
	if err := json.NewDecoder(resp.Body).Decode(&jackettResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("✅ Jackett returned %d results for query: %s\n", len(jackettResp.Results), query)

	// Cache the results if cache is available
	if j.cache != nil && j.searchTTL > 0 {
		cacheKey := j.generateCacheKey(query)
		j.cache.Set(cacheKey, jackettResp.Results, j.searchTTL)
	}

	return jackettResp.Results, nil
}

// Scrape performs the scraping operation
func (j *JackettScraper) Scrape(ctx context.Context, request types.ScrapeRequest, torrentMgr TorrentManager) ([]types.ScrapeResult, error) {
	var queries []string
	if request.MediaType == "movie" {
		queries = append(queries, request.Title)
	} else if request.MediaType == "series" && request.Episode != nil {
		queries = append(queries, fmt.Sprintf("%s s%02d", request.Title, request.Season))
		queries = append(queries, fmt.Sprintf("%s complet", request.Title))
		queries = append(queries, fmt.Sprintf("%s pack", request.Title))
		if request.Season != 1 {
			queries = append(queries, fmt.Sprintf("%s s01-", request.Title))
		}
	}

	// Use a wait group to fetch all queries concurrently
	var wg sync.WaitGroup
	resultsChan := make(chan []JackettResult, len(queries))
	errorsChan := make(chan error, len(queries))

	// Fetch results for all queries concurrently
	for _, query := range queries {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			results, err := j.fetchJackettResults(ctx, q)
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
	var allResults []JackettResult
	seen := make(map[string]bool)

	matcher := NewTitleMatcher(85)
	for results := range resultsChan {
		for _, result := range results {
			// Deduplicate by Details field
			if !seen[result.Details] {
				seen[result.Details] = true

				// Filter by title match
				if !matcher.Matches(request.Title, result.Title) {
					log.Printf("🚫 Title mismatch: expected '%s', got '%s'", request.Title, result.Title)
					continue
				}

				// Filter out season packs when looking for specific episodes
				if request.MediaType == "series" {
					if shouldFilterSeriesResult(result, request) {
						continue
					}
				}

				allResults = append(allResults, result)
			}
		}
	}

	// Log any errors
	for err := range errorsChan {
		fmt.Printf("Warning: Error fetching Jackett results: %v\n", err)
	}

	// Process all torrents concurrently
	var processingWg sync.WaitGroup
	torrentsChan := make(chan []types.ScrapeResult, len(allResults))

	for _, result := range allResults {
		processingWg.Add(1)
		go func(r JackettResult) {
			defer processingWg.Done()
			torrents, err := j.processTorrent(ctx, r, request.MediaOnlyID, request.Season, torrentMgr)
			if err != nil {
				fmt.Printf("Warning: Error processing torrent %s: %v\n", r.Title, err)
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
func (j *JackettScraper) getCachedHash(link string) (hash string, sources []string) {
	cacheKey := fmt.Sprintf("hash_%s", link)
	cached, found := j.cache.Get(cacheKey)
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

// downloadAndExtractHash downloads torrent file and extracts hash/trackers
func (j *JackettScraper) downloadAndExtractHash(
	ctx context.Context,
	link string,
	torrentMgr TorrentManager,
) (hash string, sources []string) {
	content, magnetHash, magnetURL, err := torrentMgr.DownloadTorrent(ctx, link)

	// Try torrent file first
	if err == nil && content != nil {
		metadata, err := torrentMgr.ExtractTorrentMetadata(content)
		if err == nil && metadata != nil {
			hash = strings.ToLower(metadata.InfoHash)
			sources = metadata.AnnounceList
			log.Printf("📥 Extracted hash from torrent file: %s", hash)
		}
	}

	// Fallback to magnet link
	if hash == "" && magnetHash != "" {
		hash = strings.ToLower(magnetHash)
		sources = torrentMgr.ExtractTrackersFromMagnet(magnetURL)
		log.Printf("🧲 Extracted hash from magnet: %s", hash)
	}

	// Cache the result if we got a hash
	if hash != "" && j.cache != nil {
		cacheKey := fmt.Sprintf("hash_%s", link)
		j.cache.SetPermanent(cacheKey, map[string]interface{}{
			"hash":    hash,
			"sources": sources,
		})
		log.Printf("💾 Cached hash for future use")
	}

	return hash, sources
}

// buildTorrentResults constructs the final result slice
func (j *JackettScraper) buildTorrentResults(
	result JackettResult,
	infoHash string,
	sources []string,
	torrentMgr TorrentManager,
	mediaID string,
	season int,
) []types.ScrapeResult {
	torrent := types.ScrapeResult{
		Title:     result.Title,
		InfoHash:  infoHash,
		FileIndex: nil,
		Seeders:   result.Seeders,
		Size:      result.Size,
		Tracker:   result.Tracker,
		Sources:   sources,
	}

	// Add to torrent queue if we have a magnet URI
	if result.MagnetUri != "" {
		if err := torrentMgr.AddTorrent(result.MagnetUri, torrent.Seeders, torrent.Tracker, mediaID, season); err != nil {
			log.Printf("⚠️ Error adding torrent to queue: %v", err)
		}
	}

	return []types.ScrapeResult{torrent}
}
