package scrapers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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
}

// JackettResponse represents the API response
type JackettResponse struct {
	Results []JackettResult `json:"Results"`
}

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

// ScrapeResult represents a processed torrent result
type ScrapeResult struct {
	Title     string   `json:"title"`
	InfoHash  string   `json:"infoHash"`
	FileIndex *int     `json:"fileIndex"`
	Seeders   *int     `json:"seeders"`
	Size      int64    `json:"size"`
	Tracker   string   `json:"tracker"`
	Sources   []string `json:"sources"`
}

// ScrapeRequest represents a scrape request
type ScrapeRequest struct {
	Title       string
	MediaType   string
	Season      int
	Episode     *int
	MediaOnlyID string
}

// JackettScraper handles scraping from Jackett
type JackettScraper struct {
	manager ScraperManager
	client  *http.Client
	url     string
	apiKey  string
}

// ScraperManager interface (you'll need to implement this based on your needs)
type ScraperManager interface {
	// Add methods as needed
}

// TorrentManager interface
type TorrentManager interface {
	AddTorrent(magnetURL string, seeders *int, tracker, mediaID string, season int) error
	DownloadTorrent(ctx context.Context, url string) (content []byte, magnetHash string, magnetURL string, error error)
	ExtractTorrentMetadata(content []byte) (*TorrentMetadata, error)
	ExtractTrackersFromMagnet(magnetURL string) []string
}

// NewJackettScraper creates a new Jackett scraper
func NewJackettScraper(manager ScraperManager, url, apiKey string) *JackettScraper {
	return &JackettScraper{
		manager: manager,
		client: &http.Client{
			Timeout: IndexerTimeout,
		},
		url:    url,
		apiKey: apiKey,
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

	// Try to download torrent file
	if result.Link != "" {
		content, magnetHash, magnetURL, err := torrentMgr.DownloadTorrent(ctx, result.Link)

		if err == nil && content != nil {
			metadata, err := torrentMgr.ExtractTorrentMetadata(content)
			if err == nil && metadata != nil {
				// Process each file in the torrent
				for _, file := range metadata.Files {
					torrent := baseTorrent
					torrent.Title = file.Name
					torrent.InfoHash = strings.ToLower(metadata.InfoHash)
					fileIdx := file.Index
					torrent.FileIndex = &fileIdx
					torrent.Size = file.Size
					torrent.Sources = metadata.AnnounceList
					torrents = append(torrents, torrent)
				}
				return torrents, nil
			}
		}

		// If we got a magnet hash, use it
		if magnetHash != "" {
			baseTorrent.InfoHash = strings.ToLower(magnetHash)
			baseTorrent.Sources = torrentMgr.ExtractTrackersFromMagnet(magnetURL)

			// Add to torrent queue
			if err := torrentMgr.AddTorrent(magnetURL, baseTorrent.Seeders, baseTorrent.Tracker, mediaID, season); err != nil {
				// Log error but continue
				fmt.Printf("Error adding torrent to queue: %v\n", err)
			}

			torrents = append(torrents, baseTorrent)
			return torrents, nil
		}
	}

	// Fall back to InfoHash if available
	if result.InfoHash != "" {
		baseTorrent.InfoHash = strings.ToLower(result.InfoHash)

		if result.MagnetUri != "" {
			baseTorrent.Sources = torrentMgr.ExtractTrackersFromMagnet(result.MagnetUri)

			// Add to torrent queue
			if err := torrentMgr.AddTorrent(result.MagnetUri, baseTorrent.Seeders, baseTorrent.Tracker, mediaID, season); err != nil {
				fmt.Printf("Error adding torrent to queue: %v\n", err)
			}
		}

		torrents = append(torrents, baseTorrent)
	}

	return torrents, nil
}

// fetchJackettResults fetches results from Jackett for a given query
func (j *JackettScraper) fetchJackettResults(ctx context.Context, query string) ([]JackettResult, error) {
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

	return jackettResp.Results, nil
}

// isSeasonPack checks if a title indicates a season pack or complete series
// It filters out titles containing season ranges, complete series, or pack indicators
func isSeasonPack(title string) bool {
	titleLower := strings.ToLower(title)
	
	// Season range patterns (e.g., "S01-S03", "S01-03", "1-3", "Temporada 1-3")
	seasonRangePatterns := []string{
		`s\d{1,2}-s?\d{1,2}`,          // S01-S03, S01-03
		`season\s*\d{1,2}-\d{1,2}`,    // Season 1-3
		`temporada\s*\d{1,2}-\d{1,2}`, // Temporada 1-3 (Portuguese)
	}
	
	for _, pattern := range seasonRangePatterns {
		matched, _ := regexp.MatchString(pattern, titleLower)
		if matched {
			return true
		}
	}
	
	// Complete series indicators
	completeSeriesKeywords := []string{
		"complete series",
		"complete season",
		"full series",
		"full season",
		"série completa",  // Portuguese
		"serie completa",  // Portuguese (alternative spelling)
		"temporada completa", // Portuguese
		"season pack",
		"season.pack",
		"show pack",
		"show.pack",
		"pack completo", // Portuguese
		"coleção completa", // Portuguese
		"colecao completa", // Portuguese (without accent)
	}
	
	for _, keyword := range completeSeriesKeywords {
		if strings.Contains(titleLower, keyword) {
			return true
		}
	}
	
	return false
}

// Scrape performs the scraping operation
func (j *JackettScraper) Scrape(ctx context.Context, request ScrapeRequest, torrentMgr TorrentManager) ([]ScrapeResult, error) {
	var queries []string
	queries = append(queries, request.Title)

	// Add additional queries for series
	if request.MediaType == "series" && request.Episode != nil {
		queries = append(queries, fmt.Sprintf("%s S%02d", request.Title, request.Season))
		queries = append(queries, fmt.Sprintf("%s S%02dE%02d", request.Title, request.Season, *request.Episode))
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
				if request.MediaType == "series" && request.Episode != nil {
					if isSeasonPack(result.Title) {
						fmt.Printf("🚫 Filtered season pack: %s\n", result.Title)
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
