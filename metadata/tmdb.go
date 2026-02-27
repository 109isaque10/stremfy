package metadata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"
)

type IMDbID struct {
	IMDbID string `json:"imdb_id"`
}
type Provider struct {
	tmdbAPIKey string
	client     *http.Client
	cache      *ttlcache.Cache[string, any]
}

type CachedMetadata struct {
	Title string
	Year  string
	Type  string // "movie" or "series"
	ID    string
}

func NewMetadataProvider(tmdbAPIKey string, cache *ttlcache.Cache[string, any]) *Provider {
	mp := &Provider{
		tmdbAPIKey: tmdbAPIKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: cache,
	}

	return mp
}

// TMDB API response structures
type TMDBFindResponse struct {
	MovieResults []TMDBMovie `json:"movie_results"`
	TVResults    []TMDBShow  `json:"tv_results"`
}

func (mp *Provider) GetTitleFromIMDb(imdbID string) (string, error) {
	// Validate IMDb ID format
	if !strings.HasPrefix(imdbID, "tt") || len(imdbID) < 4 {
		return imdbID, fmt.Errorf("invalid IMDb ID format: %s", imdbID)
	}

	// Check cache first
	if cached := mp.cache.Get(imdbID); cached != nil {
		value := cached.Value().(*CachedMetadata)
		zap.L().Debug("📦 Cache hit", zap.String("IMDbID", imdbID), zap.String("title", value.Title), zap.String("id", value.ID), zap.String("mediaType", value.Type), zap.String("year", value.Year))
		return value.Title, nil
	}

	// Try TMDB
	if mp.tmdbAPIKey != "" {
		title, mediaType, year, id, err := mp.getTitleFromTMDB(imdbID)
		if err == nil && title != "" {
			mp.CacheSet(imdbID, title, year, mediaType, strconv.Itoa(id))
			zap.L().Debug("✅ Found title", zap.String("IMDbID", imdbID), zap.String("title", title), zap.Int("id", id), zap.String("year", year), zap.String("mediaType", mediaType))
			return title, nil
		}
		zap.L().Error("TMDB lookup failed", zap.String("IMDbID", imdbID), zap.String("title", title), zap.Int("id", id), zap.String("year", year), zap.String("mediaType", mediaType), zap.Error(err))
	}

	// Fallback to IMDb ID
	return imdbID, fmt.Errorf("unable to fetch title for %s", imdbID)
}

func (mp *Provider) getTitleFromTMDB(imdbID string) (title, mediaType, year string, id int, err error) {
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

	zap.L().Debug("🔍 Fetching metadata from TMDB", zap.String("IMDbID", imdbID), zap.String("title", title), zap.Int("id", id), zap.String("year", year), zap.String("mediaType", mediaType))

	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Add user agent
	req.Header.Set("User-Agent", "TorBox-Stremio-Addon/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := mp.client.Do(req)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
		}
	}(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return "", "", "", 0, fmt.Errorf("TMDB API key is invalid")
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", "", "", 0, fmt.Errorf("TMDB rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", "", 0, fmt.Errorf("TMDB API error: status %d", resp.StatusCode)
	}

	var result TMDBFindResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", 0, fmt.Errorf("failed to decode response: %w", err)
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

		zap.L().Debug("✅ Found movie", zap.String("IMDbID", imdbID), zap.String("title", title), zap.Int("id", id), zap.String("year", year))
		return title, mediaType, year, movie.ID, nil
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

		zap.L().Debug("✅ Found TV show", zap.String("IMDbID", imdbID), zap.String("title", title), zap.Int("id", id), zap.String("year", year))
		return title, mediaType, year, show.ID, nil
	}

	return "", "", "", 0, fmt.Errorf("no results found for %s", imdbID)
}

// GetMetadataFromTMDB gets full metadata including title, year, type
func (mp *Provider) GetMetadataFromTMDB(imdbID string) (*CachedMetadata, error) {
	// Check cache first
	if cached := mp.cache.Get(imdbID); cached != nil {
		value := cached.Value().(*CachedMetadata)
		return value, nil
	}

	// Fetch from TMDB
	title, mediaType, year, id, err := mp.getTitleFromTMDB(imdbID)
	if err != nil {
		return nil, err
	}

	metadata := &CachedMetadata{
		Title: title,
		Year:  year,
		Type:  mediaType,
	}

	// Cache it
	mp.CacheSet(imdbID, title, year, mediaType, strconv.Itoa(id))

	return metadata, nil
}

func (mp *Provider) CacheSet(imdbID, title, year, mediaType string, id string) {
	cachedMetadata := &CachedMetadata{
		Title: title,
		Year:  year,
		Type:  mediaType,
		ID:    id,
	}

	mp.cache.Set(imdbID, cachedMetadata, ttlcache.NoTTL)
}

func (mp *Provider) GetIMDbID(ctx context.Context, mediaType, id string) (string, error) {
	// TMDB Find endpoint - finds movies/shows by external ID (IMDb)
	apiURL := fmt.Sprintf(
		"https://api.themoviedb.org/3/%s/%s/external_ids",
		url.QueryEscape(mediaType),
		url.QueryEscape(id),
	)

	// Build query parameters
	params := url.Values{}
	params.Set("api_key", mp.tmdbAPIKey)
	params.Set("language", "en-US")

	fullURL := apiURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return "", err
	}

	// Add user agent
	req.Header.Set("User-Agent", "TorBox-Stremio-Addon/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := mp.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("TMDB API key is invalid")
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("TMDB rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("TMDB API error: status %d", resp.StatusCode)
	}

	var result IMDbID
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.IMDbID, nil
}
