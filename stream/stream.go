package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Manifest defines the addon manifest
type Manifest struct {
	ID            string         `json:"id"`
	Version       string         `json:"version"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Resources     []string       `json:"resources"`
	Types         []string       `json:"types"`
	Catalogs      []Catalog      `json:"catalogs,omitempty"`
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

// Catalog defines a catalog in the manifest
type Catalog struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Extra []ExtraProperty `json:"extra,omitempty"`
}

// ExtraProperty defines extra properties for catalogs
type ExtraProperty struct {
	Name         string   `json:"name"`
	IsRequired   bool     `json:"isRequired,omitempty"`
	Options      []string `json:"options,omitempty"`
	OptionsLimit int      `json:"optionsLimit,omitempty"`
}

// MetaItem represents a meta item in catalog or meta response
type MetaItem struct {
	ID            string             `json:"id"`
	Type          string             `json:"type"`
	Name          string             `json:"name"`
	Poster        string             `json:"poster,omitempty"`
	PosterShape   string             `json:"posterShape,omitempty"`
	Background    string             `json:"background,omitempty"`
	Logo          string             `json:"logo,omitempty"`
	Description   string             `json:"description,omitempty"`
	ReleaseInfo   string             `json:"releaseInfo,omitempty"`
	IMDbRating    string             `json:"imdbRating,omitempty"`
	Released      string             `json:"released,omitempty"`
	Links         []MetaLink         `json:"links,omitempty"`
	Videos        []Video            `json:"videos,omitempty"`
	Runtime       string             `json:"runtime,omitempty"`
	Language      string             `json:"language,omitempty"`
	Country       string             `json:"country,omitempty"`
	Awards        string             `json:"awards,omitempty"`
	Website       string             `json:"website,omitempty"`
	BehaviorHints *MetaBehaviorHints `json:"behaviorHints,omitempty"`
}

// MetaBehaviorHints provides hints for meta items
type MetaBehaviorHints struct {
	DefaultVideoID     string `json:"defaultVideoId,omitempty"`
	HasScheduledVideos bool   `json:"hasScheduledVideos,omitempty"`
}

// MetaLink represents a link in meta item
type MetaLink struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	URL      string `json:"url"`
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
	Title       string   `json:"title,omitempty"`
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

// CatalogResponse is the response for catalog requests
type CatalogResponse struct {
	Metas []MetaItem `json:"metas"`
}

// MetaResponse is the response for meta requests
type MetaResponse struct {
	Meta MetaItem `json:"meta"`
}

// StreamResponse is the response for stream requests
type StreamResponse struct {
	Streams []Stream `json:"streams"`
}

// StreamRequest represents a parsed stream request
type StreamRequest struct {
	Type    string // movie or series
	ID      string // IMDb ID
	Season  int    // for series
	Episode int    // for series
}

// Addon represents a Stremio addon
type Addon struct {
	manifest       Manifest
	catalogHandler func(catalogType, catalogID string, extra map[string]string) (*CatalogResponse, error)
	metaHandler    func(metaType, id string) (*MetaResponse, error)
	streamHandler  func(req StreamRequest) (*StreamResponse, error)
}

// NewAddon creates a new Stremio addon
func NewAddon(manifest Manifest) *Addon {
	return &Addon{
		manifest: manifest,
	}
}

// SetCatalogHandler sets the catalog handler
func (a *Addon) SetCatalogHandler(handler func(catalogType, catalogID string, extra map[string]string) (*CatalogResponse, error)) {
	a.catalogHandler = handler
}

// SetMetaHandler sets the meta handler
func (a *Addon) SetMetaHandler(handler func(metaType, id string) (*MetaResponse, error)) {
	a.metaHandler = handler
}

// SetStreamHandler sets the stream handler
func (a *Addon) SetStreamHandler(handler func(req StreamRequest) (*StreamResponse, error)) {
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sdk":   "go",
			"addon": a.manifest.Name,
		})
		return
	}

	// Manifest endpoint
	if parts[0] == "manifest. json" {
		json.NewEncoder(w).Encode(a.manifest)
		return
	}

	// Catalog endpoint:  /catalog/: type/:id[/: extra]. json
	if len(parts) >= 3 && parts[0] == "catalog" && strings.HasSuffix(parts[len(parts)-1], ".json") {
		a.handleCatalog(w, r, parts)
		return
	}

	// Meta endpoint: /meta/:type/:id. json
	if len(parts) == 3 && parts[0] == "meta" && strings.HasSuffix(parts[2], ".json") {
		a.handleMeta(w, r, parts)
		return
	}

	// Stream endpoint: /stream/:type/:id. json or /stream/:type/:id: season: episode.json
	if len(parts) == 3 && parts[0] == "stream" && strings.HasSuffix(parts[2], ".json") {
		a.handleStream(w, r, parts)
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
}

// handleCatalog handles catalog requests
func (a *Addon) handleCatalog(w http.ResponseWriter, r *http.Request, parts []string) {
	if a.catalogHandler == nil {
		http.Error(w, "Catalog not supported", http.StatusNotImplemented)
		return
	}

	catalogType := parts[1]
	catalogID := parts[2]

	extra := make(map[string]string)
	if len(parts) > 3 {
		extraStr := strings.TrimSuffix(parts[3], ".json")
		pairs := strings.Split(extraStr, "&")
		for _, pair := range pairs {
			kv := strings.Split(pair, "=")
			if len(kv) == 2 {
				extra[kv[0]] = kv[1]
			}
		}
	} else {
		catalogID = strings.TrimSuffix(catalogID, ".json")
	}

	response, err := a.catalogHandler(catalogType, catalogID, extra)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(response)
}

// handleMeta handles meta requests
func (a *Addon) handleMeta(w http.ResponseWriter, r *http.Request, parts []string) {
	if a.metaHandler == nil {
		http.Error(w, "Meta not supported", http.StatusNotImplemented)
		return
	}

	metaType := parts[1]
	id := strings.TrimSuffix(parts[2], ".json")

	response, err := a.metaHandler(metaType, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(response)
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

	response, err := a.streamHandler(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	matched, _ := regexp.MatchString(`^tt\d+$`, imdbID)
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
