package debrid

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"stremfy/types"
	"strings"
	"time"
)

const (
	baseURL = "https://api.torbox.app/v1/api"
)

// API endpoints
const (
	downloadPath = "/torrents/requestdl"
	removePath   = "/torrents/controltorrent"
	statsPath    = "/user/me"
	historyPath  = "/torrents/mylist"
	explorePath  = "/torrents/mylist?id=%s"
	cachePath    = "/torrents/checkcached"
	cloudPath    = "/torrents/createtorrent"
)

// Client represents a TorBox API client
type Client struct {
	name         string
	apiKey       string
	userAgent    string
	sortPriority string
	storeToCloud bool
	timeout      time.Duration
	httpClient   *http.Client
	cache        types.Cache
	cacheTTL     time.Duration
}

// Config holds configuration for the TorBox client
type Config struct {
	APIKey       string
	SortPriority string
	StoreToCloud bool
	Timeout      time.Duration
	Cache        types.Cache
	CacheTTL     time.Duration
}

// NewClient creates a new TorBox client
func NewClient(config Config) *Client {
	if config.Timeout == 0 {
		config.Timeout = 28 * time.Second
	}

	return &Client{
		name:         "TorBox",
		apiKey:       config.APIKey,
		userAgent:    "Mozilla/5.0",
		sortPriority: config.SortPriority,
		storeToCloud: config.StoreToCloud,
		timeout:      config.Timeout,
		httpClient: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
				MaxIdleConnsPerHost: 10,
			},
		},
		cache:    config.Cache,
		cacheTTL: config.CacheTTL,
	}
}

// Response structures
type APIResponse struct {
	Success bool            `json:"success"`
	Detail  string          `json:"detail,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type AccountInfo struct {
	Email            string `json:"email"`
	Customer         string `json:"customer"`
	Plan             int    `json:"plan"`
	PremiumExpiresAt string `json:"premium_expires_at"`
	TotalDownloaded  int64  `json:"total_downloaded"`
}

type TorrentFile struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Size      int64  `json:"size"`
}

type TorrentInfo struct {
	ID               int           `json:"id"`
	Name             string        `json:"name"`
	Hash             string        `json:"hash"`
	DownloadState    string        `json:"download_state"`
	DownloadSpeed    float64       `json:"download_speed"`
	UploadSpeed      float64       `json:"upload_speed"`
	TotalDownloaded  int64         `json:"total_downloaded"`
	TotalUploaded    int64         `json:"total_uploaded"`
	Size             int64         `json:"size"`
	Seeds            int           `json:"seeds"`
	Files            []TorrentFile `json:"files"`
	UpdatedAt        string        `json:"updated_at"`
	DownloadFinished bool          `json:"download_finished"`
}

type CacheCheck struct {
	Hash  string           `json:"hash"`
	Files []CachedFileInfo `json:"files,omitempty"`
}

type CachedFileInfo struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Index int    `json:"index"`
}

type SelectedFile struct {
	Link     string `json:"link"`
	Filename string `json:"filename"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
}

// request makes an HTTP request to the TorBox API
func (c *Client) request(method, path string, params url.Values, formData url.Values) ([]byte, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	fullURL := baseURL + path
	if params != nil && len(params) > 0 {
		fullURL += "?" + params.Encode()
	}
	fullURL, _ = url.QueryUnescape(fullURL)

	req, err := http.NewRequest(method, fullURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	if formData != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// get makes a GET request
func (c *Client) get(path string, params url.Values) ([]byte, error) {
	return c.request(http.MethodGet, path, params, nil)
}

// post makes a POST request
func (c *Client) post(path string, params url.Values, formData url.Values) ([]byte, error) {
	return c.request(http.MethodPost, path, params, formData)
}

// AccountInfo retrieves account information
func (c *Client) AccountInfo() (*AccountInfo, error) {
	data, err := c.get(statsPath, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Success bool        `json:"success"`
		Data    AccountInfo `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API request failed")
	}

	return &response.Data, nil
}

// TorrentInfo retrieves information about a specific torrent
func (c *Client) TorrentInfo(requestID string) (*TorrentInfo, error) {
	path := fmt.Sprintf(explorePath, requestID)
	data, err := c.get(path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Success bool        `json:"success"`
		Data    TorrentInfo `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response.Data, nil
}

// DeleteTorrent deletes a torrent
func (c *Client) DeleteTorrent(requestID string) error {
	//body := map[string]interface{}{
	//	"torrent_id": requestID,
	//	"operation":  "delete",
	//}
	params := url.Values{}
	params.Set("torrent_id", requestID)
	params.Set("operation", "delete")

	_, err := c.post(removePath, nil, params)
	return err
}

// GetDownloadLink gets a direct download link for a file in a cached torrent
func (c *Client) GetDownloadLink(hash string, fileIndex int) (string, error) {
	// First, we need to add the torrent (if not already added)
	// For cached torrents, this is instant
	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)

	torrentID, err := c.AddMagnet(magnet)
	if err != nil {
		return "", fmt.Errorf("failed to add magnet: %w", err)
	}

	// Now get the download link using requestdl
	params := url.Values{}
	params.Set("token", c.apiKey)
	params.Set("torrent_id", torrentID)
	params.Set("file_id", fmt.Sprintf("%d", fileIndex))

	data, err := c.get(downloadPath, params)
	if err != nil {
		return "", fmt.Errorf("failed to get download link: %w", err)
	}

	var response struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Success {
		return "", fmt.Errorf("failed to get download link")
	}

	return response.Data, nil
}

// GetTorrentFiles gets the list of files in a torrent
func (c *Client) GetTorrentFiles(hash string) ([]CachedFileInfo, string, error) {
	// Add the torrent to get its ID (instant for cached torrents)
	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)

	torrentID, err := c.AddMagnet(magnet)
	if err != nil {
		return nil, "", fmt.Errorf("failed to add magnet: %w", err)
	}

	// Get torrent info with file list
	torrentInfo, err := c.TorrentInfo(torrentID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get torrent info: %w", err)
	}

	// Convert to CachedFileInfo
	var files []CachedFileInfo
	for _, file := range torrentInfo.Files {
		files = append(files, CachedFileInfo{
			Name:  file.Name,
			Size:  file.Size,
			Index: file.ID,
		})
	}

	return files, torrentID, nil
}

// UnrestrictLink unrestricts a torrent link
func (c *Client) UnrestrictLink(fileID string) (string, error) {
	parts := strings.Split(fileID, ",")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid file ID format")
	}

	params := url.Values{}
	params.Set("token", c.apiKey)
	params.Set("torrent_id", parts[0])
	params.Set("file_id", parts[1])

	data, err := c.get(downloadPath, params)
	if err != nil {
		return "", err
	}

	var response struct {
		Data string `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response.Data, nil
}

// CheckCacheSingle checks if a single hash is cached
func (c *Client) CheckCacheSingle(hash string) ([]CacheCheck, error) {
	params := url.Values{}
	params.Set("hash", hash)
	params.Set("format", "list")

	data, err := c.get(cachePath, params)
	if err != nil {
		return nil, err
	}

	var response struct {
		Success bool         `json:"success"`
		Data    []CacheCheck `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response.Data, nil
}

// generateCacheKey generates a cache key for hash check requests
func (c *Client) generateCacheKey(hashes []string) string {
	hashesStr := strings.Join(hashes, ",")
	hash := sha256.Sum256([]byte(hashesStr))
	return fmt.Sprintf("torbox_cache_%x", hash)
}

// CheckCache checks if multiple hashes are cached
func (c *Client) CheckCache(hashes []string) ([]CacheCheck, error) {
	// Check cache first if available
	if c.cache != nil {
		cacheKey := c.generateCacheKey(hashes)
		if cached, found := c.cache.Get(cacheKey); found {
			if results, ok := cached.([]CacheCheck); ok {
				fmt.Printf("📦 Cache hit for TorBox cache check (%d hashes)\n", len(hashes))
				return results, nil
			}
		}
	}

	params := url.Values{}
	params.Set("format", "list")
	params.Set("hash", strings.Join(hashes, ","))

	//body := map[string]interface{}{
	//	"hashes": hashes,
	//}

	data, err := c.post(cachePath, params, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Success bool         `json:"success"`
		Data    []CacheCheck `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Cache the results if cache is available
	if c.cache != nil && c.cacheTTL > 0 {
		cacheKey := c.generateCacheKey(hashes)
		c.cache.Set(cacheKey, response.Data, c.cacheTTL)
	}

	return response.Data, nil
}

// AddMagnet adds a magnet link
func (c *Client) AddMagnet(magnet string) (string, error) {
	//body := map[string]interface{}{
	//	"magnet":             magnet,
	//	"seed":               1,
	//	"allow_zip":          false,
	//	"add_only_if_cached": true,
	//}
	params := url.Values{}
	params.Set("magnet", magnet)
	params.Set("seed", "1")
	params.Set("allow_zip", "false")

	data, err := c.post(cloudPath, nil, params)
	if err != nil {
		return "", err
	}

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			TorrentID int `json:"torrent_id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Success {
		return "", fmt.Errorf("failed to add magnet")
	}

	return fmt.Sprintf("%d", response.Data.TorrentID), nil
}

// UserCloud retrieves user's cloud torrents
func (c *Client) UserCloud(requestID string) ([]TorrentInfo, error) {
	path := historyPath
	if requestID != "" {
		path = fmt.Sprintf(explorePath, requestID)
	}

	data, err := c.get(path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Success bool          `json:"success"`
		Data    []TorrentInfo `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response.Data, nil
}

// AddHeadersToURL adds headers to a URL
func (c *Client) AddHeadersToURL(rawURL string) string {
	headers := url.Values{}
	headers.Set("User-Agent", c.userAgent)
	return rawURL + "|" + headers.Encode()
}

// FormatBytes converts bytes to human-readable format
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
