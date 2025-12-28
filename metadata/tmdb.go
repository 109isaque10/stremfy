package metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Provider struct {
	tmdbAPIKey string
	client     *http.Client
	cache      *Cache
	cacheTTL   time.Duration
}

type Cache struct {
	mu    sync.RWMutex
	items map[string]*CachedMetadata
}

type CachedMetadata struct {
	Title     string
	Year      string
	Type      string // "movie" or "series"
	ExpiresAt time.Time
}

func NewMetadataProvider(tmdbAPIKey string, cacheTTL time.Duration) *Provider {
	if cacheTTL == 0 {
		cacheTTL = 24 * time.Hour // Default to 24 hours
	}

	mp := &Provider{
		tmdbAPIKey: tmdbAPIKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: &Cache{
			items: make(map[string]*CachedMetadata),
		},
		cacheTTL: cacheTTL,
	}

	// Start cache cleanup goroutine
	mp.cache.StartCleanup(1 * time.Hour)

	return mp
}

// TMDB API response structures
type TMDBFindResponse struct {
	MovieResults []TMDBMovie `json:"movie_results"`
	TVResults    []TMDBShow  `json:"tv_results"`
}

type TMDBMovie struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   string  `json:"release_date"`
	Overview      string  `json:"overview"`
	VoteAverage   float64 `json:"vote_average"`
	Popularity    float64 `json:"popularity"`
}

type TMDBShow struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	FirstAirDate string  `json:"first_air_date"`
	Overview     string  `json:"overview"`
	VoteAverage  float64 `json:"vote_average"`
	Popularity   float64 `json:"popularity"`
}

func (mp *Provider) GetTitleFromIMDb(imdbID string) (string, error) {
	// Validate IMDb ID format
	if !strings.HasPrefix(imdbID, "tt") || len(imdbID) < 4 {
		return imdbID, fmt.Errorf("invalid IMDb ID format: %s", imdbID)
	}

	// Check cache first
	if cached := mp.cache.Get(imdbID); cached != nil {
		log.Printf("📦 Cache hit for %s: %s", imdbID, cached.Title)
		return cached.Title, nil
	}

	// Try TMDB
	if mp.tmdbAPIKey != "" {
		title, mediaType, year, err := mp.getTitleFromTMDB(imdbID)
		if err == nil && title != "" {
			mp.cache.Set(imdbID, title, year, mediaType, mp.cacheTTL)
			log.Printf("✅ Found title for %s: %s (%s)", imdbID, title, year)
			return title, nil
		}
		log.Printf("⚠️  TMDB lookup failed for %s: %v", imdbID, err)
	}

	// Fallback to IMDb ID
	return imdbID, fmt.Errorf("unable to fetch title for %s", imdbID)
}

func (mp *Provider) getTitleFromTMDB(imdbID string) (title, mediaType, year string, err error) {
	// TMDB Find endpoint - finds movies/shows by external ID (IMDb)
	apiURL := fmt.Sprintf(
		"https://api.themoviedb.org/3/find/%s",
		url.QueryEscape(imdbID),
	)

	// Build query parameters
	params := url.Values{}
	params.Set("api_key", mp.tmdbAPIKey)
	params.Set("external_source", "imdb_id")
	params.Set("language", "en-US")

	fullURL := apiURL + "?" + params.Encode()

	log.Printf("🔍 Fetching metadata from TMDB for %s", imdbID)

	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add user agent
	req.Header.Set("User-Agent", "TorBox-Stremio-Addon/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := mp.client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
		}
	}(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return "", "", "", fmt.Errorf("TMDB API key is invalid")
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", "", "", fmt.Errorf("TMDB rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("TMDB API error: status %d", resp.StatusCode)
	}

	var result TMDBFindResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Check movie results first
	if len(result.MovieResults) > 0 {
		movie := result.MovieResults[0]
		title = movie.Title
		mediaType = "movie"

		// Extract year from release date (format: YYYY-MM-DD)
		if movie.ReleaseDate != "" && len(movie.ReleaseDate) >= 4 {
			year = movie.ReleaseDate[:4]
		}

		log.Printf("✅ Found movie: %s (%s)", title, year)
		return title, mediaType, year, nil
	}

	// Check TV show results
	if len(result.TVResults) > 0 {
		show := result.TVResults[0]
		title = show.Name
		mediaType = "series"

		// Extract year from first air date (format: YYYY-MM-DD)
		if show.FirstAirDate != "" && len(show.FirstAirDate) >= 4 {
			year = show.FirstAirDate[:4]
		}

		log.Printf("✅ Found TV show: %s (%s)", title, year)
		return title, mediaType, year, nil
	}

	return "", "", "", fmt.Errorf("no results found for %s", imdbID)
}

// GetMetadataFromTMDB gets full metadata including title, year, type
func (mp *Provider) GetMetadataFromTMDB(imdbID string) (*CachedMetadata, error) {
	// Check cache first
	if cached := mp.cache.Get(imdbID); cached != nil {
		return cached, nil
	}

	// Fetch from TMDB
	title, mediaType, year, err := mp.getTitleFromTMDB(imdbID)
	if err != nil {
		return nil, err
	}

	metadata := &CachedMetadata{
		Title: title,
		Year:  year,
		Type:  mediaType,
	}

	// Cache it
	mp.cache.Set(imdbID, title, year, mediaType, mp.cacheTTL)

	return metadata, nil
}

// Cache methods
func (c *Cache) Get(imdbID string) *CachedMetadata {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if item, exists := c.items[imdbID]; exists {
		if time.Now().Before(item.ExpiresAt) {
			return item
		}
		// Expired
		delete(c.items, imdbID)
	}

	return nil
}

func (c *Cache) Set(imdbID, title, year, mediaType string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[imdbID] = &CachedMetadata{
		Title:     title,
		Year:      year,
		Type:      mediaType,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*CachedMetadata)
}

// StartCleanup starts periodic cleanup of expired cache entries
func (c *Cache) StartCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			c.cleanup()
		}
	}()
}

func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	count := 0
	for id, item := range c.items {
		if now.After(item.ExpiresAt) {
			delete(c.items, id)
			count++
		}
	}

	if count > 0 {
		log.Printf("🧹 Cleaned up %d expired cache entries", count)
	}
}

// GetCacheStats returns cache statistics
func (c *Cache) GetCacheStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"total_entries": len(c.items),
		"entries":       []map[string]string{},
	}

	for id, item := range c.items {
		stats["entries"] = append(stats["entries"].([]map[string]string), map[string]string{
			"imdb_id": id,
			"title":   item.Title,
			"year":    item.Year,
			"type":    item.Type,
		})
	}

	return stats
}
