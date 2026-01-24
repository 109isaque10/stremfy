package metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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

	log.Printf("🔍 Fetching details from TMDB for %s", id)

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

		log.Printf("✅ Found TV show: %s (%s)", result.Name, result.Year)
		return result, nil
	}

	log.Printf("TMDB API error: %s", result.Status)

	return TMDBShowDetails{}, fmt.Errorf("no results found for %s", id)
}
