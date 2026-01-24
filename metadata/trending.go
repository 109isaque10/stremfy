package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type TMDBTrendingResponse struct {
	Results []TMDBTrendingItem `json:"results"`
}

type TMDBTrendingItem struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`                    // For movies
	Name         string `json:"name"`                     // For TV shows
	MediaType    string `json:"media_type"`               // "movie" or "tv"
	ReleaseDate  string `json:"release_date,omitempty"`   // For movies
	FirstAirDate string `json:"first_air_date,omitempty"` // For TV shows
	TotalSeasons int
}

func (mp *Provider) FetchTrendingMovies(ctx context.Context) ([]TMDBTrendingItem, error) {
	url := fmt.Sprintf("https://api.themoviedb.org/3/trending/movie/week?api_key=%s", mp.tmdbAPIKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := mp.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API error: %d", resp.StatusCode)
	}

	var result TMDBTrendingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	log.Printf("📽️ Found %d trending movies", len(result.Results))
	return result.Results, nil
}

func (mp *Provider) FetchTrendingTV(ctx context.Context) ([]TMDBTrendingItem, error) {
	url := fmt.Sprintf("https://api.themoviedb.org/3/trending/tv/week?api_key=%s", mp.tmdbAPIKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := mp.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API error:  %d", resp.StatusCode)
	}

	var result TMDBTrendingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	log.Printf("📺 Found %d trending TV shows", len(result.Results))

	return result.Results, nil
}
