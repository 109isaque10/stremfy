package scrapers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	IndexerTimeout = 60 * time.Second
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
	manager     ScraperManager
	client      *http.Client
	url         string
	apiKey      string
	searchCache SearchCache
	hashCache   HashCache
	searchTTL   time.Duration
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
func NewJackettScraper(manager ScraperManager, url, apiKey string, searchCache SearchCache, hashCache HashCache, searchTTL time.Duration) *JackettScraper {
	return &JackettScraper{
		manager: manager,
		client: &http.Client{
			Timeout: IndexerTimeout,
		},
		url:         url,
		apiKey:      apiKey,
		searchCache: searchCache,
		hashCache:   hashCache,
		searchTTL:   searchTTL,
	}
}

// processTorrent processes a single torrent result
func (j *JackettScraper) processTorrent(
	ctx context.Context,
	result JackettResult,
	mediaID string,
	season int,
	torrentMgr TorrentManager,
) ([]ScrapeResult, error) {
	baseTorrent := ScrapeResult{
		Title:     result.Title,
		InfoHash:  "",
		FileIndex: nil,
		Seeders:   result.Seeders,
		Size:      result.Size,
		Tracker:   result.Tracker,
		Sources:   []string{},
	}

	var torrents []ScrapeResult

	// Get the info hash first
	var infoHash string
	var sources []string

	// Check hash cache first if we have a Link
	if result.Link != "" && j.hashCache != nil {
		cacheKey := fmt.Sprintf("hash_%s", result.Link)
		if cached, found := j.hashCache.Get(cacheKey); found {
			if hashData, ok := cached.(map[string]interface{}); ok {
				if hash, ok := hashData["hash"].(string); ok {
					infoHash = hash
					if src, ok := hashData["sources"].([]string); ok {
						sources = src
					}
					fmt.Printf("📦 Cache hit for hash: %s\n", result.Link)
				}
			}
		}
	}

	// Try to download torrent file to get hash and sources if not cached
	if infoHash == "" && result.Link != "" {
		var content []byte
		var magnetHash string
		var magnetURL string
		var err error
		content, magnetHash, magnetURL, err = torrentMgr.DownloadTorrent(ctx, result.Link)
		if err == nil && content != nil {
			metadata, err := torrentMgr.ExtractTorrentMetadata(content)
			if err == nil && metadata != nil {
				infoHash = strings.ToLower(metadata.InfoHash)
				sources = metadata.AnnounceList

				// Cache the hash permanently
				if j.hashCache != nil {
					cacheKey := fmt.Sprintf("hash_%s", result.Link)
					j.hashCache.SetPermanent(cacheKey, map[string]interface{}{
						"hash":    infoHash,
						"sources": sources,
					})
				}
			}
		} else if magnetHash != "" {
			// If we got a magnet hash, use it
			infoHash = strings.ToLower(magnetHash)
			sources = torrentMgr.ExtractTrackersFromMagnet(magnetURL)

			// Cache the hash permanently
			if j.hashCache != nil {
				cacheKey := fmt.Sprintf("hash_%s", result.Link)
				j.hashCache.SetPermanent(cacheKey, map[string]interface{}{
					"hash":    infoHash,
					"sources": sources,
				})
			}
		}
	}

	// If we don't have an info hash, we can't proceed
	if infoHash == "" {
		fmt.Printf("⏭️  Skipping torrent %s: no info hash available\n", result.Title)
		return torrents, nil
	}

	baseTorrent.InfoHash = infoHash
	baseTorrent.Sources = sources

	// Add to torrent queue if we have a magnet URI
	if result.MagnetUri != "" {
		if err := torrentMgr.AddTorrent(result.MagnetUri, baseTorrent.Seeders, baseTorrent.Tracker, mediaID, season); err != nil {
			fmt.Printf("Error adding torrent to queue: %v\n", err)
		}
	}

	torrents = append(torrents, baseTorrent)

	return torrents, nil
}

// generateCacheKey generates a cache key for a search query
func (j *JackettScraper) generateCacheKey(query string) string {
	hash := sha256.Sum256([]byte(query))
	return fmt.Sprintf("jackett_search_%x", hash)
}

// fetchJackettResults fetches results from Jackett for a given query
func (j *JackettScraper) fetchJackettResults(ctx context.Context, query string) ([]JackettResult, error) {
	// Check cache first if cache is available
	if j.searchCache != nil {
		cacheKey := j.generateCacheKey(query)
		if cached, found := j.searchCache.Get(cacheKey); found {
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
	if j.searchCache != nil && j.searchTTL > 0 {
		cacheKey := j.generateCacheKey(query)
		j.searchCache.Set(cacheKey, jackettResp.Results, j.searchTTL)
	}

	return jackettResp.Results, nil
}

// Scrape performs the scraping operation
func (j *JackettScraper) Scrape(ctx context.Context, request ScrapeRequest, torrentMgr TorrentManager) ([]ScrapeResult, error) {
	var queries []string
	if request.MediaType == "movie" {
		queries = append(queries, request.Title)
	} else if request.MediaType == "series" && request.Episode != nil {
		queries = append(queries, fmt.Sprintf("%s S%02d", request.Title, request.Season))
		queries = append(queries, fmt.Sprintf("%s complet", request.Title))
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

	for results := range resultsChan {
		for _, result := range results {
			// Deduplicate by Details field
			if !seen[result.Details] {
				seen[result.Details] = true

				// Filter out season packs when looking for specific episodes
				if request.MediaType == "series" {
					if isSeasonPack(result.Title, request.Season) {
						fmt.Printf("🚫 Filtered season pack: %s\n", result.Title)
						continue
					} else if isEpisodePack(result.Title, request.Season, *request.Episode) {
						fmt.Printf("🚫 Filtered episode pack: %s\n", result.Title)
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
	torrentsChan := make(chan []ScrapeResult, len(allResults))

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
	var finalTorrents []ScrapeResult
	for torrents := range torrentsChan {
		for _, torrent := range torrents {
			if torrent.InfoHash != "" {
				finalTorrents = append(finalTorrents, torrent)
			}
		}
	}

	return finalTorrents, nil
}
