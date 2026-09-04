package debrid

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"stremfy/types"
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
	explorePath  = "/torrents/mylist?id=%s"
	cachePath    = "/torrents/checkcached"
	cloudPath    = "/torrents/createtorrent"
	webCache     = "/webdl/checkcached"
	webCloud     = "/webdl/createwebdownload"
	webExplore   = "/webdl/mylist?id=%s"
	webDownload  = "/webdl/requestdl"
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

type CacheResponse struct {
	Success bool         `json:"success"`
	Data    []CacheCheck `json:"data"`
}

type SelectedFile struct {
	Link     string `json:"link"`
	Filename string `json:"filename"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
}

type TorrentInfoResponse struct {
	Success bool        `json:"success"`
	Data    TorrentInfo `json:"data"`
}

type WebCloudResponse struct {
	Success bool `json:"success"`
	Data    struct {
		WebDownloadID int `json:"webdownload_id"`
	} `json:"data"`
}

type TorrentCloudResponse struct {
	Success bool `json:"success"`
	Data    struct {
		TorrentID int `json:"torrent_id"`
	} `json:"data"`
}

func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
	defer memStore.Close() // Ensure the store is closed when the client is done
}

// request makes an HTTP request to the TorBox API
func (c *Client) request(method, path string, params url.Values, formData url.Values) (io.ReadCloser, error) {
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return resp.Body, nil
}

// get makes a GET request
func (c *Client) get(path string, params url.Values) (io.ReadCloser, error) {
	return c.request(http.MethodGet, path, params, nil)
}

// post makes a POST request
func (c *Client) post(path string, params url.Values, formData url.Values) (io.ReadCloser, error) {
	return c.request(http.MethodPost, path, params, formData)
}

// TorrentInfo retrieves information about a specific torrent
func (c *Client) torrentInfo(requestID string) (*TorrentInfo, error) {
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
	defer data.Close()

	var response TorrentInfoResponse

	if err := json.NewDecoder(data).Decode(&response); err != nil {
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

func (c *Client) webInfo(requestID string) (*TorrentInfo, error) {
	startTime := time.Now()

	if c.cache != nil {
		cacheKey := "webInfo_" + requestID
		if cached := c.cache.Get(cacheKey); cached != nil {
			if result, ok := cached.Value().(*TorrentInfo); ok {
				zap.L().Debug(fmt.Sprintf("📦 Cache hit for TorBox webInfo (RequestID %s)", requestID))
				if c.timeLogging {
					endTime := time.Since(startTime)
					zap.L().Debug("---FUNC---> WebInfo", zap.String("requestID", requestID))
					zap.L().Debug(fmt.Sprintf("---TIME---> WebInfo %dms", endTime.Milliseconds()))
				}
				return result, nil
			}
		}
	}

	path := fmt.Sprintf(webExplore, requestID)
	data, err := c.get(path, nil)
	if err != nil {
		return nil, err
	}
	defer data.Close()

	var response TorrentInfoResponse

	if err := json.NewDecoder(data).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> WebInfo", zap.String("requestID", requestID))
		zap.L().Debug(fmt.Sprintf("---TIME---> WebInfo %dms", endTime.Milliseconds()))
	}

	webInfo := &response.Data

	// Cache the results if cache is available
	if c.cache != nil {
		cacheKey := "webInfo_" + requestID
		c.cache.Set(cacheKey, webInfo, ttlcache.NoTTL)
	}

	return webInfo, nil
}

func (c *Client) FetchFiles(hash string, t types.SourceType) ([]CachedFileInfo, string, error) {
	switch t {
	case types.TORRENT:
		return c.getTorrentFiles(hash)
	case types.DDL:
		return c.getWebFiles(hash)
	default:
		return nil, "", fmt.Errorf("unsupported source type: %s", t)
	}
}

// GetTorrentFiles gets the list of files in a torrent
func (c *Client) getTorrentFiles(hash string) ([]CachedFileInfo, string, error) {
	// Add the torrent to get its ID (instant for cached torrents)
	startTime := time.Now()
	torrentID, err := c.addMagnet(hash)
	if err != nil {
		return nil, "", fmt.Errorf("failed to add magnet: %w", err)
	}

	// Get torrent info with file list
	torrentInfo, err := c.torrentInfo(torrentID)
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

func (c *Client) getWebFiles(link string) ([]CachedFileInfo, string, error) {
	// Add the torrent to get its ID (instant for cached torrents)
	startTime := time.Now()
	webID, err := c.addLink(link)
	if err != nil {
		return nil, "", fmt.Errorf("failed to add link: %w", err)
	}

	// Get torrent info with file list
	webInfo, err := c.webInfo(webID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get web info: %w", err)
	}

	// Convert to CachedFileInfo
	var files []CachedFileInfo
	for _, file := range webInfo.Files {
		files = append(files, CachedFileInfo{
			Name:  file.Name,
			Size:  file.Size,
			Index: file.ID,
		})
	}

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> GetWebFiles", zap.String("link", link))
		zap.L().Debug(fmt.Sprintf("---TIME---> GetWebFiles %dms", endTime.Milliseconds()))
	}

	return files, webID, nil
}

func (c *Client) UnrestrictLink(fileID string, t types.SourceType) (string, error) {
	switch t {
	case types.TORRENT:
		return c.unrestrictTorrentLink(fileID)
	case types.DDL:
		return c.unrestrictWebLink(fileID)
	default:
		return "", fmt.Errorf("unsupported source type: %s", t)
	}
}

// UnrestrictLink unrestricts a torrent link
func (c *Client) unrestrictTorrentLink(fileID string) (string, error) {
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
	defer data.Close()

	var response struct {
		Data string `json:"data"`
	}

	if err := json.NewDecoder(data).Decode(&response); err != nil {
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

func (c *Client) unrestrictWebLink(fileID string) (string, error) {
	_, directExists := os.LookupEnv("DIRECT_TORBOX")

	if c.cache != nil && !directExists {
		cacheKey := "streamweblink_" + fileID
		if cached := c.cache.Get(cacheKey); cached != nil {
			if result, ok := cached.Value().(string); ok {
				zap.L().Debug(fmt.Sprintf("📦 Cache hit for TorBox unrestrictWebLink (FileID %s)", fileID))
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
	params.Set("web_id", parts[0])
	params.Set("file_id", parts[1])

	if directExists {
		params.Set("redirect", "true")
		params.Set("append_name", "true")

		return baseURL + webDownload + "?" + params.Encode(), nil
	}

	startTime := time.Now()

	data, err := c.get(webDownload, params)
	if err != nil {
		return "", err
	}
	defer data.Close()

	var response struct {
		Data string `json:"data"`
	}

	if err := json.NewDecoder(data).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> UnrestrictWebLink", zap.String("fileID", fileID))
		zap.L().Debug(fmt.Sprintf("---TIME---> UnrestrictWebLink %dms", endTime.Milliseconds()))
	}

	// Cache the results for 3h (Streamlink links are valid for 3h, so we cache for a bit less to be safe)
	if c.cache != nil {
		cacheKey := "streamweblink_" + fileID
		c.cache.Set(cacheKey, response.Data, 220*time.Minute)
	}

	return response.Data, nil
}

// generateCacheKey generates a cache key for hash check requests
func (c *Client) generateCacheKey(hashes []string) string {
	hashesStr := strings.Join(hashes, ",")
	hash := sha256.Sum256([]byte(hashesStr))
	return fmt.Sprintf("torbox_cache_%x", hash)
}

func (c *Client) CheckCache(hashes []string, t types.SourceType) ([]CacheCheck, error) {
	switch t {
	case types.TORRENT:
		return c.checkTorrentCache(hashes)
	case types.DDL:
		return c.checkLinkCache(hashes)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", t)
	}
}

func (c *Client) checkLinkCache(hashes []string) ([]CacheCheck, error) {
	params := url.Values{}
	params.Set("format", "list")
	params.Set("hash", strings.Join(hashes, ","))

	startTime := time.Now()

	data, err := c.get(webCache, params)
	if err != nil {
		return nil, err
	}
	defer data.Close()

	var response CacheResponse

	if err := json.NewDecoder(data).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> CheckLinksCache", zap.Strings("hashes", hashes))
		zap.L().Debug(fmt.Sprintf("---TIME---> CheckLinksCache %dms", endTime.Milliseconds()))
	}

	return response.Data, nil
}

// CheckCache checks if multiple hashes are cached
func (c *Client) checkTorrentCache(hashes []string) ([]CacheCheck, error) {

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
	defer data.Close()

	var response CacheResponse

	if err := json.NewDecoder(data).Decode(&response); err != nil {
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
func (c *Client) addMagnet(hash string) (string, error) {
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

	var response TorrentCloudResponse

	if err := json.NewDecoder(data).Decode(&response); err != nil {
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

func (c *Client) addLink(link string) (string, error) {
	//body := map[string]interface{}{
	//	"magnet":             magnet,
	//	"seed":               1,
	//	"allow_zip":          false,
	//	"add_only_if_cached": true,
	//}

	if c.cache != nil {
		cacheKey := "linkID_" + link
		if cached := c.cache.Get(cacheKey); cached != nil {
			if result, ok := cached.Value().(string); ok {
				zap.L().Debug(fmt.Sprintf("📦 Cache hit for TorBox addLink (Link %s)", link))
				return result, nil
			}
		}
	}

	startTime := time.Now()
	hits := 0

	for !c.rateLimiter.Allow("addLink") && hits < 10 {
		time.Sleep(600 * time.Millisecond) // Wait before retrying
		hits++
		zap.L().Debug("⏳ Rate limit hit for AddLink, waiting...")
	}
	if hits >= 10 {
		return "", fmt.Errorf("rate limit exceeded for AddLink after multiple retries")
	}
	zap.L().Debug("✅ Allowed by rate limiter for AddLink")

	params := url.Values{}
	params.Set("link", link)
	params.Set("add_only_if_cached", "true")

	data, err := c.post(webCloud, nil, params)
	if err != nil {
		return "", err
	}

	var response WebCloudResponse

	if err := json.NewDecoder(data).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Success {
		return "", fmt.Errorf("failed to add link")
	}

	if c.timeLogging {
		endTime := time.Since(startTime)
		zap.L().Debug("---FUNC---> AddLink", zap.String("link", link))
		zap.L().Debug(fmt.Sprintf("---TIME---> AddLink %dms", endTime.Milliseconds()))
	}

	webID := strconv.Itoa(response.Data.WebDownloadID)

	// Cache the results if cache is available
	if c.cache != nil {
		cacheKey := "webID_" + link
		c.cache.Set(cacheKey, webID, ttlcache.NoTTL)
	}

	return webID, nil
}

// AddHeadersToURL adds headers to a URL
func (c *Client) addHeadersToURL(rawURL string) string {
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

func (c *Client) HashLink(link string) string {
	if c.cache != nil {
		cacheKey := "linkhash_" + link
		if cached := c.cache.Get(cacheKey); cached != nil {
			if result, ok := cached.Value().(string); ok {
				zap.L().Debug(fmt.Sprintf("📦 Cache hit for TorBox hashLink (Link %s)", link))
				return result
			}
		}
	}

	hash := md5.New()
	hash.Write([]byte(link))
	result := hex.EncodeToString(hash.Sum(nil))
	if c.cache != nil {
		cacheKey := "linkhash_" + link
		c.cache.Set(cacheKey, result, ttlcache.NoTTL)
	}
	return result
}
