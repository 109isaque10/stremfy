package metadata

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/goccy/go-json"
	"go.uber.org/zap"
)

type TMDBShow struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	FirstAirDate string `json:"first_air_date"`
}

type TMDBShowDetails struct {
	Status          string `json:"status_message,omitempty"`
	ID              int    `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	OriginalName    string `json:"original_name,omitempty"`
	FirstAirDate    string `json:"first_air_date,omitempty"`
	NumberOfSeasons int    `json:"number_of_seasons,omitempty"`
	Year            string
}

func (mp *Provider) GetTVShowDetails(id string) (tvShow TMDBShowDetails, err error) {
	// TMDB Find endpoint - finds movies/shows by external ID (IMDb)
	apiURL := fmt.Sprintf(
		"https://api.themoviedb.org/3/tv/%s",
		url.QueryEscape(id),
	)

	// Build query parameters
	params := url.Values{}
	params.Set("api_key", mp.tmdbAPIKey)
	params.Set("language", "en-US")

	fullURL := apiURL + "?" + params.Encode()

	zap.L().Debug("🔍 Fetching details from TMDB", zap.String("id", id))

	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return TMDBShowDetails{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add user agent
	req.Header.Set("User-Agent", "TorBox-Stremio-Addon/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := mp.client.Do(req)
	if err != nil {
		return TMDBShowDetails{}, fmt.Errorf("request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
		}
	}(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return TMDBShowDetails{}, fmt.Errorf("TMDB API key is invalid")
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return TMDBShowDetails{}, fmt.Errorf("TMDB rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
		return TMDBShowDetails{}, fmt.Errorf("TMDB API error: status %d", resp.StatusCode)
	}

	var result TMDBShowDetails
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return TMDBShowDetails{}, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check TV show results
	if result.ID != 0 {

		// Extract year from first air date (format: YYYY-MM-DD)
		if result.FirstAirDate != "" && len(result.FirstAirDate) >= 4 {
			result.Year = result.FirstAirDate[:4]
		}

		zap.L().Debug("✅ Found TV show", zap.String("name", result.Name), zap.Int("id", result.ID), zap.String("year", result.Year), zap.String("originalName", result.OriginalName), zap.Int("numberOfSeasons", result.NumberOfSeasons))
		return result, nil
	}

	zap.L().Error("TMDB API error", zap.String("status", result.Status))

	return TMDBShowDetails{}, fmt.Errorf("no results found for %s", id)
}
