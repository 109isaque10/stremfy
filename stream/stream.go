package stream

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
	"github.com/wasilibs/go-re2"
)

// Manifest defines the addon manifest
type Manifest struct {
	ID            string         `json:"id"`
	Version       string         `json:"version"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Resources     []string       `json:"resources"`
	Types         []string       `json:"types"`
	IDPrefixes    []string       `json:"idPrefixes,omitempty"`
	Background    string         `json:"background,omitempty"`
	Logo          string         `json:"logo,omitempty"`
	ContactEmail  string         `json:"contactEmail,omitempty"`
	BehaviorHints *BehaviorHints `json:"behaviorHints,omitempty"`
}

// BehaviorHints provides hints about addon behavior
type BehaviorHints struct {
	Adult                 bool `json:"adult,omitempty"`
	P2P                   bool `json:"p2p,omitempty"`
	Configurable          bool `json:"configurable,omitempty"`
	ConfigurationRequired bool `json:"configurationRequired,omitempty"`
}

// Video represents a video (episode for series)
type Video struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Released  string   `json:"released,omitempty"`
	Season    int      `json:"season,omitempty"`
	Episode   int      `json:"episode,omitempty"`
	Thumbnail string   `json:"thumbnail,omitempty"`
	Overview  string   `json:"overview,omitempty"`
	Streams   []Stream `json:"streams,omitempty"`
}

// Stream represents a stream source
type Stream struct {
	// Required fields
	URL      string `json:"url,omitempty"`
	YTId     string `json:"ytId,omitempty"`
	InfoHash string `json:"infoHash,omitempty"`
	FileIdx  int    `json:"fileIdx,omitempty"`

	// Optional fields
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	ExternalURL string   `json:"externalUrl,omitempty"`
	Sources     []string `json:"sources,omitempty"`

	// Metadata
	BehaviorHints *StreamBehaviorHints `json:"behaviorHints,omitempty"`
}

// StreamBehaviorHints provides hints for streams
type StreamBehaviorHints struct {
	BingeGroup       string   `json:"bingeGroup,omitempty"`
	CountryWhitelist []string `json:"countryWhitelist,omitempty"`
	NotWebReady      bool     `json:"notWebReady,omitempty"`
	VideoSize        int64    `json:"videoSize,omitempty"`
	VideoHash        string   `json:"videoHash,omitempty"`
	Filename         string   `json:"filename,omitempty"`
}

// StreamResponse is the response for stream requests
type StreamResponse struct {
	Streams []Stream `json:"streams"`
}

// StreamRequest represents a parsed stream request
type StreamRequest struct {
	Title            string // content title
	Type             string // movie or series
	ID               string // IMDb ID
	Season           int    // for series
	Episode          int    // for series
	Year             string
	AlternativeTitle string
}

// Addon represents a Stremio addon
type Addon struct {
	manifest      Manifest
	streamHandler func(req StreamRequest) *StreamResponse
}

var imdbRegex = re2.MustCompile(`^tt\d+$`)

// NewAddon creates a new Stremio addon
func NewAddon(manifest Manifest) *Addon {
	return &Addon{
		manifest: manifest,
	}
}

// SetStreamHandler sets the stream handler
func (a *Addon) SetStreamHandler(handler func(req StreamRequest) *StreamResponse) {
	a.streamHandler = handler
}

// ServeHTTP implements http.Handler
func (a *Addon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	// Root endpoint
	if path == "" || path == "/" {
		json.NewEncoder(w).Encode(map[string]any{
			"sdk":   "go",
			"addon": a.manifest.Name,
		})
		return
	}

	// Manifest endpoint
	if parts[0] == "manifest.json" {
		json.NewEncoder(w).Encode(a.manifest)
		return
	}

	// Stream endpoint: /stream/:type/:id. json or /stream/:type/:id: season: episode.json
	if len(parts) == 3 && parts[0] == "stream" && strings.HasSuffix(parts[2], ".json") {
		a.handleStream(w, r, parts)
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
}

// handleStream handles stream requests
func (a *Addon) handleStream(w http.ResponseWriter, r *http.Request, parts []string) {
	if a.streamHandler == nil {
		http.Error(w, "Stream not supported", http.StatusNotImplemented)
		return
	}

	streamType := parts[1]
	idPart := strings.TrimSuffix(parts[2], ".json")

	req := StreamRequest{
		Type: streamType,
	}

	// Parse ID (format: imdb_id or imdb_id:season:episode)
	idParts := strings.Split(idPart, ":")
	req.ID = idParts[0]

	if len(idParts) >= 3 {
		season, err := strconv.Atoi(idParts[1])
		if err != nil {
			http.Error(w, "Invalid season", http.StatusBadRequest)
			return
		}
		episode, err := strconv.Atoi(idParts[2])
		if err != nil {
			http.Error(w, "Invalid episode", http.StatusBadRequest)
			return
		}
		req.Season = season
		req.Episode = episode
	}

	response := a.streamHandler(req)
	if response == nil {
		http.Error(w, "failed response", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(response)
}

// ParseStreamID is a helper to parse stream ID from various formats
func ParseStreamID(id string) (imdbID string, season, episode int, err error) {
	// Format: tt1234567 or tt1234567:1: 1
	parts := strings.Split(id, ":")

	imdbID = parts[0]

	// Validate IMDb ID format
	matched := imdbRegex.MatchString(imdbID)
	if !matched {
		err = fmt.Errorf("invalid IMDb ID format: %s", imdbID)
		return
	}

	if len(parts) >= 3 {
		season, err = strconv.Atoi(parts[1])
		if err != nil {
			err = fmt.Errorf("invalid season: %s", parts[1])
			return
		}
		episode, err = strconv.Atoi(parts[2])
		if err != nil {
			err = fmt.Errorf("invalid episode: %s", parts[2])
			return
		}
	}

	return
}

// IsMovie checks if a request is for a movie
func (r StreamRequest) IsMovie() bool {
	return r.Type == "movie"
}

// IsSeries checks if a request is for a series
func (r StreamRequest) IsSeries() bool {
	return r.Type == "series"
}

// String returns a string representation of the request
func (r StreamRequest) String() string {
	if r.IsSeries() {
		return fmt.Sprintf("%s:%d:%d", r.ID, r.Season, r.Episode)
	}
	return r.ID
}
