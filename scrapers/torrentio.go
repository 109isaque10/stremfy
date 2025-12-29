package scrapers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TorrentioResult represents a result from Torrentio API
type TorrentioResult struct {
	Title    string `json:"title"`
	InfoHash string `json:"infoHash"`
}

// TorrentioResponse represents the API response
type TorrentioResponse struct {
	Results []TorrentioResult `json:"streams"`
}

// TorrentioScraper handles scraping from Torrentio
type TorrentioScraper struct {
	manager     ScraperManager
	client      *http.Client
	url         string
	searchCache SearchCache
	hashCache   HashCache
	searchTTL   time.Duration
}

// ScraperManager interface (you'll need to implement this based on your needs)
type ScraperManager interface {
	// Add methods as needed
}

// NewTorrentioScraper creates a new Torrentio scraper
func NewTorrentioScraper(manager ScraperManager, url string, searchCache SearchCache, hashCache HashCache, searchTTL time.Duration) *TorrentioScraper {
	return &TorrentioScraper{
		manager: manager,
		client: &http.Client{
			Timeout: IndexerTimeout,
		},
		url:         url,
		searchCache: searchCache,
		hashCache:   hashCache,
		searchTTL:   searchTTL,
	}
}

// processTorrent processes a single torrent result
func (j *TorrentioScraper) processTorrent(
	ctx context.Context,
	result TorrentioResult,
	mediaID string,
	season int,
	torrentMgr TorrentManager,
) ([]ScrapeResult, error) {
	baseTorrent := ScrapeResult{
		Title:    result.Title,
		InfoHash: "",
	}

	var torrents []ScrapeResult

	// Get the info hash first
	var infoHash string
	var sources []string

	// Fall back to InfoHash if available
	if result.InfoHash != "" {
		infoHash = strings.ToLower(result.InfoHash)
	}

	// If we don't have an info hash, we can't proceed
	if infoHash == "" {
		fmt.Printf("⏭️  Skipping torrent %s: no info hash available\n", result.Title)
		return torrents, nil
	}

	baseTorrent.InfoHash = infoHash
	baseTorrent.Sources = sources

	torrents = append(torrents, baseTorrent)

	return torrents, nil
}

// generateCacheKey generates a cache key for a search query
func (j *TorrentioScraper) generateCacheKey(query string) string {
	hash := sha256.Sum256([]byte(query))
	return fmt.Sprintf("torrentio_search_%x", hash)
}

// fetchTorrentioResults fetches results from Torrentio for a given query
func (j *TorrentioScraper) fetchTorrentioResults(ctx context.Context, query string) ([]TorrentioResult, error) {
	// Check cache first if cache is available
	if j.searchCache != nil {
		cacheKey := j.generateCacheKey(query)
		if cached, found := j.searchCache.Get(cacheKey); found {
			if results, ok := cached.([]TorrentioResult); ok {
				fmt.Printf("📦 Cache hit for Torrentio search: %s\n", query)
				return results, nil
			}
		}
	}

	apiURL := fmt.Sprintf("%s/%s", j.url, query)

	fmt.Printf("🔍 Torrentio search: %s\n", query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:146.0) Gecko/20100101 Firefox/146.0")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var torrentioResp TorrentioResponse
	if err := json.NewDecoder(resp.Body).Decode(&torrentioResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("✅ Torrentio returned %d results for query: %s\n", len(torrentioResp.Results), query)

	// Cache the results if cache is available
	if j.searchCache != nil && j.searchTTL > 0 {
		cacheKey := j.generateCacheKey(query)
		j.searchCache.Set(cacheKey, torrentioResp.Results, j.searchTTL)
	}

	return torrentioResp.Results, nil
}

// Scrape performs the scraping operation
func (j *TorrentioScraper) Scrape(ctx context.Context, request ScrapeRequest, torrentMgr TorrentManager) ([]ScrapeResult, error) {
	var queries []string
	if request.MediaType == "movie" {
		queries = append(queries, fmt.Sprintf("movies/%s.json", request.MediaOnlyID))
	} else if request.MediaType == "series" && request.Episode != nil {
		queries = append(queries, fmt.Sprintf("series/%s:%d:%d.json", request.MediaOnlyID, request.Season, *request.Episode))
	}

	// Use a wait group to fetch all queries concurrently
	var wg sync.WaitGroup
	resultsChan := make(chan []TorrentioResult, len(queries))
	errorsChan := make(chan error, len(queries))

	// Fetch results for all queries concurrently
	for _, query := range queries {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			results, err := j.fetchTorrentioResults(ctx, q)
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
	var allResults []TorrentioResult
	seen := make(map[string]bool)

	for results := range resultsChan {
		for _, result := range results {
			// Deduplicate by InfoHash field
			if !seen[result.InfoHash] {
				seen[result.InfoHash] = true

				allResults = append(allResults, result)
			}
		}
	}

	// Log any errors
	for err := range errorsChan {
		fmt.Printf("Warning: Error fetching Torrentio results: %v\n", err)
	}

	// Process all torrents concurrently
	var processingWg sync.WaitGroup
	torrentsChan := make(chan []ScrapeResult, len(allResults))

	for _, result := range allResults {
		processingWg.Add(1)
		go func(r TorrentioResult) {
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
	var finalTorrents []ScrapeResult
	for torrents := range torrentsChan {
		for _, torrent := range torrents {
			if torrent.InfoHash != "" {
				title := strings.Split(torrent.Title, "\n")[0]
				seeders, _ := strconv.Atoi(strings.Split(strings.Split(torrent.Title, "👤 ")[1], " 💾")[0])
				size := strings.Split(strings.Split(torrent.Title, "💾 ")[1], " ⚙️")[0]
				torrent.Size = parseSize(size)
				torrent.Tracker = strings.Split(strings.Split(torrent.Title, "⚙️ ")[1], "\n")[0]
				torrent.Title = title
				torrent.Seeders = &seeders

				finalTorrents = append(finalTorrents, torrent)
			}
		}
	}

	return finalTorrents, nil
}
