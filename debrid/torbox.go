package debrid

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/jellydator/ttlcache/v3"
	"github.com/yasserelgammal/rate-limiter/limiter"
	"github.com/yasserelgammal/rate-limiter/store"
	"go.uber.org/zap"
)

const (
	baseURL = "https://api.torbox.app/v1/api"
)

const (
	maxRequests  = 10               // Maximum number of requests per minute
	fillInterval = time.Minute / 10 // Interval to add one token (600ms for 10/min)
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
	cache        *ttlcache.Cache[string, any]
	cacheTTL     time.Duration
	timeLogging  bool
	rateLimiter  *limiter.TokenBucket
}

// Config holds configuration for the TorBox client
type TorboxConfig struct {
	APIKey       string
	SortPriority string
	StoreToCloud bool
	Timeout      time.Duration
	Cache        *ttlcache.Cache[string, any]
	CacheTTL     time.Duration
}

var memStore *store.MemoryStore

// NewClient creates a new TorBox client
func NewClient(config TorboxConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 28 * time.Second
	}

	_, timeExists := os.LookupEnv("TIME_LOGGING")

	// 10 requests per minute with burst of 20
	rateConfig := limiter.Config{
		Rate:     maxRequests,
		Duration: time.Minute,
		Burst:    maxRequests * 2,
	}

	memStore = store.NewMemoryStore(5 * time.Minute)

	rateLimiter, err := limiter.NewTokenBucket(rateConfig, memStore)
	if err != nil {
		panic(fmt.Sprintf("Failed to create rate limiter: %v", err))
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
		cache:       config.Cache,
		cacheTTL:    config.CacheTTL,
		timeLogging: timeExists,
		rateLimiter: rateLimiter,
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
	Index int    `json:"index,omitempty"`
}

type SelectedFile struct {
	Link     string `json:"link"`
	Filename string `json:"filename"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
}

func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
	defer memStore.Close() // Ensure the store is closed when the client is done
}

// request makes an HTTP request to the TorBox API
func (c *Client) request(method, path string, params url.Values, formData url.Values) ([]byte, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	fullURL := baseURL + path
	if len(params) > 0 {
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
	startTime := time.Now()

	if c.cache != nil {
		cacheKey := "torrentInfo_" + requestID
		if cached := c.cache.Get(cacheKey); cached != nil {
			if result, ok := cached.Value().(*TorrentInfo); ok {
				zap.L().Debug(fmt.Sprintf("📦 Cache hit for TorBox torrentInfo (RequestID %s)", requestID))
				if c.timeLogging {
					endTime := time.Since(startTime)
					zap.L().Debug("---FUNC---> TorrentInfo", zap.String("requestID", requestID))
					zap.L().Debug(fmt.Sprintf("---TIME---> TorrentInfo %dms", endTime.Milliseconds()))
				}
				return result, nil
			}
		}
	}

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

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> TorrentInfo", zap.String("requestID", requestID))
		zap.L().Debug(fmt.Sprintf("---TIME---> TorrentInfo %dms", endTime.Milliseconds()))
	}

	torrentInfo := &response.Data

	// Cache the results if cache is available
	if c.cache != nil {
		cacheKey := "torrentInfo_" + requestID
		c.cache.Set(cacheKey, torrentInfo, ttlcache.NoTTL)
	}

	return torrentInfo, nil
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

	_, directExists := os.LookupEnv("DIRECT_TORBOX")

	if directExists {
		params.Set("redirect", "true")
		params.Set("append_name", "true")
		return baseURL + downloadPath + "?" + params.Encode(), nil
	}

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
	startTime := time.Now()
	torrentID, err := c.AddMagnet(hash)
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

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> GetTorrentFiles", zap.String("hash", hash))
		zap.L().Debug(fmt.Sprintf("---TIME---> GetTorrentFiles %dms", endTime.Milliseconds()))
	}

	return files, torrentID, nil
}

// UnrestrictLink unrestricts a torrent link
func (c *Client) UnrestrictLink(fileID string) (string, error) {
	_, directExists := os.LookupEnv("DIRECT_TORBOX")

	if c.cache != nil && !directExists {
		cacheKey := "streamlink_" + fileID
		if cached := c.cache.Get(cacheKey); cached != nil {
			if result, ok := cached.Value().(string); ok {
				zap.L().Debug(fmt.Sprintf("📦 Cache hit for TorBox unrestrictLink (FileID %s)", fileID))
				return result, nil
			}
		}
	}

	parts := strings.Split(fileID, ",")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid file ID format")
	}

	params := url.Values{}
	params.Set("token", c.apiKey)
	params.Set("torrent_id", parts[0])
	params.Set("file_id", parts[1])

	if directExists {
		params.Set("redirect", "true")
		params.Set("append_name", "true")

		return baseURL + downloadPath + "?" + params.Encode(), nil
	}

	startTime := time.Now()

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

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> UnrestrictLink", zap.String("fileID", fileID))
		zap.L().Debug(fmt.Sprintf("---TIME---> UnrestrictLink %dms", endTime.Milliseconds()))
	}

	// Cache the results for 3h (Streamlink links are valid for 3h, so we cache for a bit less to be safe)
	if c.cache != nil {
		cacheKey := "streamlink_" + fileID
		c.cache.Set(cacheKey, response.Data, 220*time.Minute)
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

	params := url.Values{}
	params.Set("format", "list")
	params.Set("hash", strings.Join(hashes, ","))

	//body := map[string]interface{}{
	//	"hashes": hashes,
	//}

	startTime := time.Now()

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

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> CheckCache", zap.Strings("hashes", hashes))
		zap.L().Debug(fmt.Sprintf("---TIME---> CheckCache %dms", endTime.Milliseconds()))
	}

	return response.Data, nil
}

// AddMagnet adds a magnet link
func (c *Client) AddMagnet(hash string) (string, error) {
	//body := map[string]interface{}{
	//	"magnet":             magnet,
	//	"seed":               1,
	//	"allow_zip":          false,
	//	"add_only_if_cached": true,
	//}

	if c.cache != nil {
		cacheKey := "torrentID_" + hash
		if cached := c.cache.Get(cacheKey); cached != nil {
			if result, ok := cached.Value().(string); ok {
				zap.L().Debug(fmt.Sprintf("📦 Cache hit for TorBox addMagnet (Hash %s)", hash))
				return result, nil
			}
		}
	}

	startTime := time.Now()
	hits := 0

	for !c.rateLimiter.Allow("addMagnet") && hits < 10 {
		time.Sleep(600 * time.Millisecond) // Wait before retrying
		hits++
		zap.L().Debug("⏳ Rate limit hit for AddMagnet, waiting...")
	}
	if hits >= 10 {
		return "", fmt.Errorf("rate limit exceeded for AddMagnet after multiple retries")
	}
	zap.L().Debug("✅ Allowed by rate limiter for AddMagnet")

	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
	params := url.Values{}
	params.Set("magnet", magnet)
	params.Set("seed", "1")
	params.Set("allow_zip", "false")
	params.Set("add_only_if_cached", "true")

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

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> AddMagnet", zap.String("magnet", magnet))
		zap.L().Debug(fmt.Sprintf("---TIME---> AddMagnet %dms", endTime.Milliseconds()))
	}

	torrentID := strconv.Itoa(response.Data.TorrentID)

	// Cache the results if cache is available
	if c.cache != nil {
		cacheKey := "torrentID_" + hash
		c.cache.Set(cacheKey, torrentID, ttlcache.NoTTL)
	}

	return torrentID, nil
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
