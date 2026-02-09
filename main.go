package main

import (
	"cmp"
	"encoding/gob"
	"net"
	"os/signal"
	"slices"
	"stremfy/types"
	"syscall"

	_ "github.com/joho/godotenv/autoload"
	"go.uber.org/zap"
	_ "golang.org/x/crypto/x509roots/fallback"
)

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"stremfy/caching"
	"stremfy/debrid"
	"stremfy/metadata"
	"stremfy/scrapers"
	"stremfy/stream"
	"stremfy/torrentManager"
	"stremfy/utils"
	"strings"
	"time"
)

func init() {
	// Force pure Go DNS resolver (no CGO)
	net.DefaultResolver.PreferGo = true
	net.DefaultResolver.Dial = nil // Use default dialer
	// Global logger
	zapConfig := zap.NewDevelopmentConfig()
	encoder := zap.NewDevelopmentEncoderConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	zapConfig.EncoderConfig = encoder
	zapConfig.Encoding = "console"
	zap.ReplaceGlobals(zap.Must(zapConfig.Build()))
	// Register all types that will be stored as interface{} in cache
	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
	gob.Register([]scrapers.JackettResult{})
	gob.Register(scrapers.JackettResult{})
	gob.Register([]scrapers.TorrProxyResult{})
	gob.Register(scrapers.TorrProxyResult{})
	gob.Register(types.ScrapeResult{})
	gob.Register([]types.ScrapeResult{})
	gob.Register([]string{})
	gob.Register(time.Time{})
}

type TorBoxStremioAddon struct {
	addon            *stream.Addon
	torboxClient     *debrid.Client
	jackettScraper   *scrapers.JackettScraper
	torrProxyScraper *scrapers.TorrProxyScraper
	metadataProvider *metadata.Provider
	cache            *caching.Cache
	backgroundWorker *caching.BackgroundWork
}

func NewTorBoxStremioAddon(torboxAPIKey, jackettURL, jackettAPIKey string, jackettEnabled bool, torrProxyURL string, torrProxyEnabled bool, tmdbAPIKey string, searchTTL, metadataTTL, torboxTTL time.Duration) *TorBoxStremioAddon {
	manifest := stream.Manifest{
		ID:          "com.stremio.stremfy",
		Version:     "1.0.0",
		Name:        "Stremfy",
		Description: "Search torrents via Jackett/TorrProxy and stream with TorBox",
		Resources:   []string{"stream"},
		Types:       []string{"movie", "series"},
		IDPrefixes:  []string{"tt"},
		Logo:        "https://torbox.app/logo.png",
		Background:  "https://torbox.app/background.jpg",
		BehaviorHints: &stream.BehaviorHints{
			P2P:                   false,
			Configurable:          false,
			ConfigurationRequired: false,
		},
	}

	addon := stream.NewAddon(manifest)

	// Initialize caches
	cache := caching.NewCache()

	logger := zap.L()

	logger.Debug("✅ Caching system initialized", zap.String("searchTTL", searchTTL.String()), zap.String("metadataTTL", metadataTTL.String()), zap.String("torboxTTL", torboxTTL.String()))

	torboxClient := debrid.NewClient(debrid.Config{
		APIKey:       torboxAPIKey,
		StoreToCloud: false,
		Timeout:      30 * time.Second,
		Cache:        cache,
		CacheTTL:     torboxTTL,
	})

	jackettScraper := scrapers.NewJackettScraper(nil, jackettURL, jackettAPIKey, cache, searchTTL, jackettEnabled)
	torrProxyScraper := scrapers.NewTorrProxyScraper(nil, torrProxyURL, cache, 5*time.Minute, torrProxyEnabled)

	var metadataProvider *metadata.Provider
	metadataProvider = metadata.NewMetadataProvider(tmdbAPIKey, metadataTTL)
	logger.Debug("✅ TMDB metadata provider initialized")

	ta := &TorBoxStremioAddon{
		addon:            addon,
		torboxClient:     torboxClient,
		jackettScraper:   jackettScraper,
		torrProxyScraper: torrProxyScraper,
		metadataProvider: metadataProvider,
		cache:            cache,
	}

	// Initialize background worker with injected dependencies
	ta.backgroundWorker = caching.NewBackgroundWorker(
		// Pass searchTorrents as a function
		func(ctx context.Context, req types.ScrapeRequest) []types.ScrapeResult {
			return ta.searchTorrents(ctx, req)
		},
		ta.metadataProvider,
	)

	addon.SetStreamHandler(ta.handleStream)

	return ta
}

func (ta *TorBoxStremioAddon) handleStream(req stream.StreamRequest) *stream.StreamResponse {
	logger := zap.L()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTime := time.Now()

	logger.Info("📺 Stream request", zap.String("id", req.ID), zap.String("type", req.Type))

	// Build search query
	searchQuery := ta.buildSearchQuery(req)

	// Search torrents
	torrents := ta.searchTorrents(ctx, searchQuery)
	if torrents == nil {
		return &stream.StreamResponse{Streams: []stream.Stream{}}
	}

	logger.Info(fmt.Sprintf("🔍 Found %d torrents", len(torrents)))

	if len(torrents) == 0 {
		return &stream.StreamResponse{Streams: []stream.Stream{}}
	}

	// Extract hashes and check TorBox cache
	streams := ta.checkCacheAndBuildStreams(torrents, req)
	if streams == nil {
		return &stream.StreamResponse{Streams: []stream.Stream{}}
	}

	endTime := time.Since(startTime)
	logger.Info(fmt.Sprintf("⏱ Took %d seconds to fetch!", int(endTime.Seconds())))

	logger.Info(fmt.Sprintf("✅ Returning %d cached streams", len(streams)))

	slices.SortFunc(streams, func(a, b stream.Stream) int {
		return cmp.Compare(a.BehaviorHints.VideoSize, b.BehaviorHints.VideoSize)
	})

	ta.backgroundWorker.UserBackgroundTask(req)

	return &stream.StreamResponse{
		Streams: streams,
	}
}

func (ta *TorBoxStremioAddon) buildSearchQuery(req stream.StreamRequest) types.ScrapeRequest {
	scrapeReq := types.ScrapeRequest{
		Title:       ta.getTitleFromIMDb(req.ID), // You'd need to implement this
		MediaType:   req.Type,
		MediaOnlyID: req.ID,
	}

	if req.IsSeries() {
		scrapeReq.Season = req.Season
		episode := req.Episode
		scrapeReq.Episode = &episode
	}

	return scrapeReq
}

func (ta *TorBoxStremioAddon) searchTorrents(ctx context.Context, query types.ScrapeRequest) []types.ScrapeResult {
	zap.L().Info(fmt.Sprintf("Started search for %s", query.Title))
	// Create a torrent manager with TorBox integration
	torrentMgr := torrentManager.NewTorrentManager(ta.torboxClient)
	// Create channels to receive results
	type searchResult struct {
		results []types.ScrapeResult
		err     error
		source  string
	}
	resultsChan := make(chan searchResult, 1)
	// Search via Jackett (async)
	if ta.jackettScraper.IsEnabled() {
		go func() {
			results, err := ta.jackettScraper.Scrape(ctx, query, torrentMgr)
			resultsChan <- searchResult{results: results, err: err, source: "jackett"}
		}()
	}
	// Search via TorrProxy (async)
	if ta.torrProxyScraper.IsEnabled() {
		go func() {
			results, err := ta.torrProxyScraper.Scrape(ctx, query, torrentMgr)
			resultsChan <- searchResult{results: results, err: err, source: "torrProxy"}
		}()
	}
	// Collect results
	var allResults []types.ScrapeResult
	result := <-resultsChan
	if result.err != nil {
		zap.L().Error("search failed", zap.String("source", result.source), zap.Error(result.err))
	} else {
		zap.L().Info(fmt.Sprintf("✅ %s returned %d results", result.source, len(result.results)))
		allResults = append(allResults, result.results...)
	}

	return allResults
}

func (ta *TorBoxStremioAddon) checkCacheAndBuildStreams(torrents []types.ScrapeResult, req stream.StreamRequest) []stream.Stream {
	logger := zap.L()
	// Extract unique hashes
	hashMap := make(map[string]types.ScrapeResult)
	var hashes []string

	logger.Info("📦 Processing torrents: ")

	for _, torrent := range torrents {
		if torrent.InfoHash != "" {
			if _, exists := hashMap[torrent.InfoHash]; !exists {
				hashMap[torrent.InfoHash] = torrent
				hashes = append(hashes, torrent.InfoHash)
			}
		}
	}

	if len(hashes) == 0 {
		return []stream.Stream{}
	}

	logger.Debug(fmt.Sprintf("🔎 Checking %d hashes in TorBox cache", len(hashes)))

	//// Check cache with TorBox
	//cached, err := ta.torboxClient.CheckCache(hashes)
	//if err != nil {
	//	logger.Error("torbox cache check failed", zap.Error(err))
	//	return nil
	//}
	cached := []debrid.CacheCheck{}

	// Build streams from cached results with file filtering
	var streams []stream.Stream
	isSeries := req.IsSeries()

	for _, item := range cached {
		hash := item.Hash
		if hash == "" {
			continue
		}

		// Get original torrent info
		torrent, exists := hashMap[hash]
		if !exists {
			continue
		}

		logger.Debug("✅ Cached torrent", zap.String("torrentTitle", torrent.Title), zap.String("hash", hash))

		// Get file list for the cached torrent
		files, torrentID, err := ta.torboxClient.GetTorrentFiles(hash)
		if err != nil {
			logger.Warn("Failed to get files, using fallback", zap.String("hash", hash), zap.Error(err))
			// Fallback to InfoHash method
			streamed := ta.buildStream(torrent, req)
			streams = append(streams, streamed)
			continue
		}

		logger.Debug(fmt.Sprintf("Found %d files in torrent", len(files)), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))

		for _, file := range files {
			// Filter 1: Must be a video file
			if !debrid.IsVideoFile(file.Name) {
				logger.Debug("⏭️ Skipping non-video file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
				continue
			}

			// Filter 2: Must meet minimum size requirements
			if !debrid.IsFileSizeValid(file.Size, isSeries) {
				logger.Debug("⏭️ Skipping file too small", zap.String("size", debrid.FormatBytes(file.Size)), zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
				continue
			}

			// Filter 3: For series, must match episode pattern
			if isSeries && !debrid.IsEpisodeFile(file.Name, req.Season, req.Episode) {
				logger.Debug("⏭️ Skipping nonEpisode file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
				continue
			}

			logger.Debug("✅ Valid file", zap.String("size", debrid.FormatBytes(file.Size)), zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))

			// Build stream with URL from requestdl
			streamed := ta.buildStreamWithURL(torrent, file, torrentID, req)
			streams = append(streams, streamed)
		}
	}

	logger.Info(fmt.Sprintf("📤 Returning %d streams after filtering", len(streams)))
	return streams
}

func (ta *TorBoxStremioAddon) buildStreamWithURL(torrent types.ScrapeResult, file debrid.CachedFileInfo, torrentID string, req stream.StreamRequest) stream.Stream {
	// Format title with quality and source info
	title := ta.formatStreamTitleWithFile(torrent, file)

	// Build file ID for download
	fileID := fmt.Sprintf("%s,%d", torrentID, file.Index)

	// Get download URL from TorBox
	downloadURL, err := ta.torboxClient.UnrestrictLink(fileID)
	if err != nil {
		zap.L().Error("Failed to get download link, falling back to InfoHash", zap.String("fileName", file.Name), zap.Error(err))
		// Fallback to InfoHash method
		return stream.Stream{
			InfoHash:    torrent.InfoHash,
			FileIdx:     file.Index,
			Description: title,
			Name:        "TorBox",
			Sources:     torrent.Sources,
			BehaviorHints: &stream.StreamBehaviorHints{
				BingeGroup:  ta.getBingeGroup(req) + torrent.InfoHash,
				VideoSize:   file.Size,
				Filename:    file.Name,
				NotWebReady: true,
			},
		}
	}

	// Return stream with direct URL
	return stream.Stream{
		URL:         downloadURL,
		Description: title,
		Name:        "TorBox",
		BehaviorHints: &stream.StreamBehaviorHints{
			BingeGroup:  ta.getBingeGroup(req) + torrent.InfoHash,
			VideoSize:   file.Size,
			Filename:    file.Name,
			NotWebReady: false,
		},
	}
}

func (ta *TorBoxStremioAddon) buildStream(torrent types.ScrapeResult, req stream.StreamRequest) stream.Stream {
	// Format title with quality and source info
	title := ta.formatStreamTitle(torrent, req)

	// Determine file index
	fileIdx := 0
	if torrent.FileIndex != nil {
		fileIdx = *torrent.FileIndex
	}

	streamed := stream.Stream{
		InfoHash:    torrent.InfoHash,
		FileIdx:     fileIdx,
		Description: title,
		Name:        "TorBox",
		Sources:     torrent.Sources,
		BehaviorHints: &stream.StreamBehaviorHints{
			BingeGroup:  ta.getBingeGroup(req) + torrent.InfoHash,
			VideoSize:   torrent.Size,
			Filename:    torrent.Title,
			NotWebReady: true,
		},
	}

	return streamed
}

func (ta *TorBoxStremioAddon) formatStreamTitle(torrent types.ScrapeResult, req stream.StreamRequest) string {
	// Extract quality from title
	quality := utils.ExtractQuality(torrent.Title)

	// Extract codec info
	codec := utils.ExtractCodec(torrent.Title)

	// Extract source info
	source := utils.ExtractSource(torrent.Title)

	// Build source info
	sourceInfo := ""
	if source != "" {
		sourceInfo = fmt.Sprintf(" 🌟 %s", source)
	}

	// Build seeders info
	seedersInfo := ""
	if torrent.Seeders != nil {
		seedersInfo = fmt.Sprintf(" 👥 %d", *torrent.Seeders)
	}

	// Build size info
	sizeInfo := ""
	if torrent.Size > 0 {
		sizeInfo = fmt.Sprintf(" 💾 %s", debrid.FormatBytes(torrent.Size))
	}

	// Build tracker info
	trackerInfo := ""
	if torrent.Tracker != "" && torrent.Tracker != "all" {
		trackerInfo = fmt.Sprintf(" [%s]", strings.Split(torrent.Tracker, " (")[0])
	}

	// Format final title
	if req.IsSeries() {
		return fmt.Sprintf("%s\n⚡ TorBox %s %s%s%s%s%s",
			torrent.Title, quality, codec, seedersInfo, sizeInfo, sourceInfo, trackerInfo)
	}

	return fmt.Sprintf("%s\n⚡ TorBox %s %s%s%s%s%s",
		torrent.Title, quality, codec, seedersInfo, sizeInfo, sourceInfo, trackerInfo)
}

func (ta *TorBoxStremioAddon) formatStreamTitleWithFile(torrent types.ScrapeResult, file debrid.CachedFileInfo) string {
	// Extract quality from filename
	quality := utils.ExtractQuality(torrent.Title)

	// Extract codec info
	codec := utils.ExtractCodec(torrent.Title)

	// Extract source info
	source := utils.ExtractSource(torrent.Title)

	// Build source info
	sourceInfo := ""
	if source != "" {
		sourceInfo = fmt.Sprintf(" 🌟 %s", source)
	}

	// Build seeders info
	seedersInfo := ""
	if torrent.Seeders != nil {
		seedersInfo = fmt.Sprintf(" 👥 %d", *torrent.Seeders)
	}

	// Build size info
	sizeInfo := fmt.Sprintf(" 💾 %s", debrid.FormatBytes(file.Size))

	// Build tracker info
	trackerInfo := ""
	if torrent.Tracker != "" && torrent.Tracker != "all" {
		trackerInfo = fmt.Sprintf(" [%s]", strings.Split(torrent.Tracker, " (")[0])
	}

	// Format final title
	return fmt.Sprintf("%s\n⚡ TorBox %s %s%s%s%s%s",
		torrent.Title, quality, codec, seedersInfo, sizeInfo, sourceInfo, trackerInfo)
}

func (ta *TorBoxStremioAddon) getTitleFromIMDb(imdbID string) string {
	// Try to get from TMDB if available
	if ta.metadataProvider != nil {
		title, err := ta.metadataProvider.GetTitleFromIMDb(imdbID)
		if err == nil && title != "" {
			return title
		}
		zap.L().Error("Failed to get title from TMDB (using IMDb ID)", zap.String("IMDbID", imdbID), zap.Error(err))
	} else {
		zap.L().Warn("Metadata provider not configured, using IMDb ID", zap.String("IMDbID", imdbID))
	}

	// Fallback to IMDb ID
	return imdbID
}

func (ta *TorBoxStremioAddon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ta.addon.ServeHTTP(w, r)
}

func (ta *TorBoxStremioAddon) getBingeGroup(req stream.StreamRequest) string {
	if req.IsSeries() {
		return fmt.Sprintf("torbox|%s|", req.ID)
	}
	return fmt.Sprintf("torbox|%s|", req.ID)
}

// getEnvDuration reads a duration from environment variable (in minutes) or returns a default
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if minutes, err := strconv.Atoi(value); err == nil {
			return time.Duration(minutes) * time.Minute
		}
		zap.L().Warn("Invalid value, using default", zap.String("key", key), zap.String("value", value))
	}
	return defaultValue
}

func gracefulShutdown(server *http.Server, addon *TorBoxStremioAddon) {
	logger := zap.L()

	logger.Info("🛑 Starting graceful shutdown...")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Shutdown HTTP server (stops accepting new connections)
	logger.Debug("🛑 Shutting down HTTP server...")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server shutdown error: %v", zap.Error(err))
	} else {
		logger.Debug("✅ HTTP server stopped")
	}

	// Stop background workers and wait for completion
	logger.Debug("🛑 Stopping background workers...")
	addon.backgroundWorker.StopAndWait()

	// Flush caches to disk
	logger.Debug("💾 Flushing caches to disk...")
	addon.cache.Flush()

	logger.Info("✅ Graceful shutdown complete")
}

func main() {
	// Force pure Go DNS resolver to avoid CGO overhead
	// This must be set before any network operations
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial:     nil,
	}
	logger := zap.L()
	defer logger.Sync()
	// Get configuration from environment variables
	torboxAPIKey := os.Getenv("TORBOX_API_KEY")
	if torboxAPIKey == "" {
		logger.Fatal("TORBOX_API_KEY environment variable is required")
	}

	jackettURL := os.Getenv("JACKETT_URL")
	if jackettURL == "" {
		jackettURL = "http://localhost:9117"
	}

	jackettEnabled, err := strconv.ParseBool(os.Getenv("JACKETT_ENABLED"))
	if err != nil {
		jackettEnabled = false
	}

	jackettAPIKey := os.Getenv("JACKETT_API_KEY")
	if jackettAPIKey == "" && jackettEnabled {
		logger.Fatal("JACKETT_API_KEY environment variable not set")
	}

	torrProxyURL := os.Getenv("TORRPROXY_URL")
	if torrProxyURL == "" {
		torrProxyURL = "http://localhost:8090"
	}

	torrProxyEnabled, err := strconv.ParseBool(os.Getenv("TORRPROXY_ENABLED"))
	if err != nil {
		torrProxyEnabled = false
	}

	tmdbAPIKey := os.Getenv("TMDB_API_KEY")
	if tmdbAPIKey == "" {
		logger.Fatal("TMDB_API_KEY environment variable is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logger.Info("✅ Server Ready!", zap.String("Port", port))

	// Get cache configuration from environment variables
	searchTTL := getEnvDuration("CACHE_SEARCH_TTL", 30*time.Minute)
	metadataTTL := getEnvDuration("CACHE_METADATA_TTL", 24*time.Hour)
	torboxTTL := getEnvDuration("CACHE_TORBOX_CHECK_TTL", 10*time.Minute)

	// Create addon
	logger.Debug("🔧 Initializing addon...")
	addon := NewTorBoxStremioAddon(torboxAPIKey, jackettURL, jackettAPIKey, jackettEnabled, torrProxyURL, torrProxyEnabled, tmdbAPIKey, searchTTL, metadataTTL, torboxTTL)
	logger.Info("✅ Addon initialized")

	// Setup HTTP server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      addon,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("🚀 Server Started", zap.String("Manifest", fmt.Sprintf("http://localhost:%s/manifest.json", port)),
		zap.String("Movie", fmt.Sprintf("http://localhost:%s/stream/movie/tt0111161.json", port)),
		zap.String("Series", fmt.Sprintf("http://localhost:%s/stream/series/tt0903747:1:1.json", port)))

	// Start server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal("Server failed", zap.Error(err))
	}

	<-sigChan
	gracefulShutdown(server, addon)
}
