package main

import (
	"sort"

	_ "github.com/joho/godotenv/autoload"
)

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"stremfy/cache"
	"stremfy/debrid"
	"stremfy/metadata"
	"stremfy/scrapers"
	"stremfy/stream"
	"stremfy/utils"
	"strings"
	"time"
)

type TorBoxStremioAddon struct {
	addon            *stream.Addon
	torboxClient     *debrid.Client
	jackettScraper   *scrapers.JackettScraper
	metadataProvider *metadata.Provider
	searchCache      *cache.Cache
	hashCache        *cache.Cache
	torboxCache      *cache.Cache
}

func NewTorBoxStremioAddon(torboxAPIKey, jackettURL, jackettAPIKey string, tmdbAPIKey string, searchTTL, metadataTTL, torboxTTL time.Duration) *TorBoxStremioAddon {
	manifest := stream.Manifest{
		ID:          "com.stremio.stremfy",
		Version:     "1.0.0",
		Name:        "TorBox + Jackett",
		Description: "Search torrents via Jackett and stream with TorBox",
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
	searchCache := cache.NewCache()
	hashCache := cache.NewCache()
	torboxCache := cache.NewCache()

	log.Println("✅ Caching system initialized")
	log.Printf("   - Search cache TTL: %v", searchTTL)
	log.Printf("   - Metadata cache TTL: %v", metadataTTL)
	log.Printf("   - TorBox cache check TTL: %v", torboxTTL)
	log.Printf("   - Hash cache: unlimited")

	torboxClient := debrid.NewClient(debrid.Config{
		APIKey:       torboxAPIKey,
		StoreToCloud: false,
		Timeout:      30 * time.Second,
		Cache:        torboxCache,
		CacheTTL:     torboxTTL,
	})

	jackettScraper := scrapers.NewJackettScraper(nil, jackettURL, jackettAPIKey, searchCache, hashCache, searchTTL)

	var metadataProvider *metadata.Provider
	metadataProvider = metadata.NewMetadataProvider(tmdbAPIKey, metadataTTL)
	log.Println("✅ TMDB metadata provider initialized")

	ta := &TorBoxStremioAddon{
		addon:            addon,
		torboxClient:     torboxClient,
		jackettScraper:   jackettScraper,
		metadataProvider: metadataProvider,
		searchCache:      searchCache,
		hashCache:        hashCache,
		torboxCache:      torboxCache,
	}

	addon.SetStreamHandler(ta.handleStream)

	return ta
}

func (ta *TorBoxStremioAddon) handleStream(req stream.StreamRequest) (*stream.StreamResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	startTime := time.Now()

	log.Printf("📺 Stream request: %s", req.String())

	// Build search query
	searchQuery := ta.buildSearchQuery(req)

	// Search torrents via Jackett
	torrents, err := ta.searchTorrents(ctx, searchQuery)
	if err != nil {
		log.Printf("❌ Error searching torrents: %v", err)
		return &stream.StreamResponse{Streams: []stream.Stream{}}, nil
	}

	log.Printf("🔍 Found %d torrents from Jackett", len(torrents))

	if len(torrents) == 0 {
		return &stream.StreamResponse{Streams: []stream.Stream{}}, nil
	}

	// Extract hashes and check TorBox cache
	streams, err := ta.checkCacheAndBuildStreams(torrents, req)
	if err != nil {
		log.Printf("❌ Error checking cache: %v", err)
		return &stream.StreamResponse{Streams: []stream.Stream{}}, nil
	}

	endTime := time.Since(startTime)
	log.Printf("⏱ Took %d seconds to fetch!\n", int(endTime.Seconds()))

	log.Printf("✅ Returning %d cached streams", len(streams))

	sort.Slice(streams, func(i, j int) bool {
		return streams[i].BehaviorHints.VideoSize > streams[j].BehaviorHints.VideoSize
	})

	return &stream.StreamResponse{
		Streams: streams,
	}, nil
}

func (ta *TorBoxStremioAddon) buildSearchQuery(req stream.StreamRequest) scrapers.ScrapeRequest {
	scrapeReq := scrapers.ScrapeRequest{
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

func (ta *TorBoxStremioAddon) searchTorrents(ctx context.Context, query scrapers.ScrapeRequest) ([]scrapers.ScrapeResult, error) {
	// Create a torrent manager with TorBox integration
	torrentMgr := utils.NewTorrentManager(ta.torboxClient)
	// Search via Jackett
	results, err := ta.jackettScraper.Scrape(ctx, query, torrentMgr)
	if err != nil {
		return nil, fmt.Errorf("jackett search failed: %w", err)
	}

	return results, nil
}

func (ta *TorBoxStremioAddon) checkCacheAndBuildStreams(torrents []scrapers.ScrapeResult, req stream.StreamRequest) ([]stream.Stream, error) {
	// Extract unique hashes
	hashMap := make(map[string]scrapers.ScrapeResult)
	var hashes []string

	log.Printf("📦 Processing torrents: ")

	for _, torrent := range torrents {
		if torrent.InfoHash != "" {
			if _, exists := hashMap[torrent.InfoHash]; !exists {
				hashMap[torrent.InfoHash] = torrent
				hashes = append(hashes, torrent.InfoHash)
			}
		}
	}

	if len(hashes) == 0 {
		return []stream.Stream{}, nil
	}

	log.Printf("🔎 Checking %d hashes in TorBox cache", len(hashes))

	// Check cache with TorBox
	cached, err := ta.torboxClient.CheckCache(hashes)
	if err != nil {
		return nil, fmt.Errorf("torbox cache check failed: %w", err)
	}

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

		log.Printf("✅ Cached torrent: %s (hash: %s)", torrent.Title, hash)

		// Get file list for the cached torrent
		files, torrentID, err := ta.torboxClient.GetTorrentFiles(hash)
		if err != nil {
			log.Printf("⚠️  Failed to get files for %s: %v, using fallback", hash, err)
			// Fallback to InfoHash method
			streamed := ta.buildStream(torrent, req)
			streams = append(streams, streamed)
			continue
		}

		log.Printf("   Found %d files in torrent (ID: %s)", len(files), torrentID)

		for _, file := range files {
			// Filter 1: Must be a video file
			if !debrid.IsVideoFile(file.Name) {
				log.Printf("   ⏭️  Skipping non-video file: %s", file.Name)
				continue
			}

			// Filter 2: Must meet minimum size requirements
			if !debrid.IsFileSizeValid(file.Size, isSeries) {
				log.Printf("   ⏭️  Skipping file too small (%s): %s", debrid.FormatBytes(file.Size), file.Name)
				continue
			}

			// Filter 3: For series, must match episode pattern
			if isSeries && !debrid.IsEpisodeFile(file.Name, req.Season, req.Episode) {
				continue
			}

			log.Printf("   ✅ Valid file: %s (%s)", file.Name, debrid.FormatBytes(file.Size))

			// Build stream with URL from requestdl
			streamed := ta.buildStreamWithURL(torrent, file, torrentID, req)
			streams = append(streams, streamed)
		}
	}

	log.Printf("📤 Returning %d streams after filtering", len(streams))
	return streams, nil
}

func (ta *TorBoxStremioAddon) buildStreamWithURL(torrent scrapers.ScrapeResult, file debrid.CachedFileInfo, torrentID string, req stream.StreamRequest) stream.Stream {
	// Format title with quality and source info
	title := ta.formatStreamTitleWithFile(torrent, file)

	// Build file ID for download
	fileID := fmt.Sprintf("%s,%d", torrentID, file.Index)

	// Get download URL from TorBox
	downloadURL, err := ta.torboxClient.UnrestrictLink(fileID)
	if err != nil {
		log.Printf("⚠️  Failed to get download link for %s: %v, falling back to InfoHash", file.Name, err)
		// Fallback to InfoHash method
		return stream.Stream{
			InfoHash: torrent.InfoHash,
			FileIdx:  file.Index,
			Title:    title,
			Name:     "TorBox",
			Sources:  torrent.Sources,
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
		URL:   downloadURL,
		Title: title,
		Name:  "TorBox",
		BehaviorHints: &stream.StreamBehaviorHints{
			BingeGroup:  ta.getBingeGroup(req) + torrent.InfoHash,
			VideoSize:   file.Size,
			Filename:    file.Name,
			NotWebReady: false,
		},
	}
}

func (ta *TorBoxStremioAddon) buildStream(torrent scrapers.ScrapeResult, req stream.StreamRequest) stream.Stream {
	// Format title with quality and source info
	title := ta.formatStreamTitle(torrent, req)

	// Determine file index
	fileIdx := 0
	if torrent.FileIndex != nil {
		fileIdx = *torrent.FileIndex
	}

	streamed := stream.Stream{
		InfoHash: torrent.InfoHash,
		FileIdx:  fileIdx,
		Title:    title,
		Name:     "TorBox",
		Sources:  torrent.Sources,
		BehaviorHints: &stream.StreamBehaviorHints{
			BingeGroup:  ta.getBingeGroup(req) + torrent.InfoHash,
			VideoSize:   torrent.Size,
			Filename:    torrent.Title,
			NotWebReady: true,
		},
	}

	return streamed
}

func (ta *TorBoxStremioAddon) formatStreamTitle(torrent scrapers.ScrapeResult, req stream.StreamRequest) string {
	// Extract quality from title
	quality := extractQuality(torrent.Title)

	// Extract codec info
	codec := extractCodec(torrent.Title)

	// Extract source info
	source := extractSource(torrent.Title)

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
		trackerInfo = fmt.Sprintf(" [%s]", torrent.Tracker)
	}

	// Format final title
	if req.IsSeries() {
		return fmt.Sprintf("⚡ TorBox %s %s%s%s%s%s",
			quality, codec, seedersInfo, sizeInfo, sourceInfo, trackerInfo)
	}

	return fmt.Sprintf("⚡ TorBox %s %s%s%s%s%s",
		quality, codec, seedersInfo, sizeInfo, sourceInfo, trackerInfo)
}

func (ta *TorBoxStremioAddon) formatStreamTitleWithFile(torrent scrapers.ScrapeResult, file debrid.CachedFileInfo) string {
	// Extract quality from filename
	quality := extractQuality(torrent.Title)

	// Extract codec info
	codec := extractCodec(torrent.Title)

	// Extract source info
	source := extractSource(torrent.Title)

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
		trackerInfo = fmt.Sprintf(" [%s]", torrent.Tracker)
	}

	// Format final title
	return fmt.Sprintf("⚡ TorBox %s %s%s%s%s%s",
		quality, codec, seedersInfo, sizeInfo, sourceInfo, trackerInfo)
}

func (ta *TorBoxStremioAddon) getTitleFromIMDb(imdbID string) string {
	// Try to get from TMDB if available
	if ta.metadataProvider != nil {
		title, err := ta.metadataProvider.GetTitleFromIMDb(imdbID)
		if err == nil && title != "" {
			return title
		}
		log.Printf("⚠️  Failed to get title from TMDB for %s: %v (using IMDb ID)", imdbID, err)
	} else {
		log.Printf("⚠️  Metadata provider not configured, using IMDb ID: %s", imdbID)
	}

	// Fallback to IMDb ID
	return imdbID
}

func (ta *TorBoxStremioAddon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ta.addon.ServeHTTP(w, r)
}

// Helper functions

func extractQuality(title string) string {
	titleLower := strings.ToLower(title)

	qualities := []struct {
		keywords []string
		label    string
	}{
		{[]string{"2160p", "4k", "uhd"}, "4K"},
		{[]string{"1080p", "fhd"}, "1080p"},
		{[]string{"720p", "hd"}, "720p"},
		{[]string{"480p"}, "480p"},
	}

	for _, q := range qualities {
		for _, kw := range q.keywords {
			if strings.Contains(titleLower, kw) {
				return q.label
			}
		}
	}

	return "Unknown"
}

func extractCodec(title string) string {
	titleLower := strings.ToLower(title)

	codecs := []struct {
		keywords []string
		label    string
	}{
		{[]string{"h265", "hevc", "x265"}, "H265"},
		{[]string{"h264", "x264", "avc"}, "H264"},
		{[]string{"av1"}, "AV1"},
		{[]string{"xvid"}, "XviD"},
	}

	for _, c := range codecs {
		for _, kw := range c.keywords {
			if strings.Contains(titleLower, kw) {
				return c.label
			}
		}
	}

	return ""
}

func extractSource(title string) string {
	titleLower := strings.ToLower(title)

	codecs := []struct {
		keywords []string
		label    string
	}{
		{[]string{"bluray", "blu-ray", "bdrip", "bd-rip", "brrip", "br-rip"}, "Source"},
		{[]string{"webdl", "web-dl", "dvdrip", "dvd-rip", "webrip", "web-rip", "dvd"}, "Premium"},
		{[]string{"screener", "scr", "tvrip", "tv-rip", "hdtv", "pdtv"}, "Standard"},
		{[]string{"cam", "camrip", "cam-rip", "telesync", "ts", "workprint", "wp"}, "Poor"},
	}

	for _, c := range codecs {
		for _, kw := range c.keywords {
			if strings.Contains(titleLower, kw) {
				return c.label
			}
		}
	}

	return ""
}

func (ta *TorBoxStremioAddon) getBingeGroup(req stream.StreamRequest) string {
	if req.IsSeries() {
		return fmt.Sprintf("torbox|%s|%d|", req.ID, req.Season)
	}
	return fmt.Sprintf("torbox|%s|", req.ID)
}

// getEnvDuration reads a duration from environment variable (in minutes) or returns a default
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if minutes, err := strconv.Atoi(value); err == nil {
			return time.Duration(minutes) * time.Minute
		}
		log.Printf("⚠️  Invalid value for %s: %s, using default", key, value)
	}
	return defaultValue
}

func main() {
	fmt.Println("===========================================")
	fmt.Println("  TorBox + Jackett Stremio Addon")
	fmt.Println("===========================================")
	fmt.Println()
	// Get configuration from environment variables
	torboxAPIKey := os.Getenv("TORBOX_API_KEY")
	if torboxAPIKey == "" {
		log.Fatal("❌ TORBOX_API_KEY environment variable is required")
	}

	jackettURL := os.Getenv("JACKETT_URL")
	if jackettURL == "" {
		jackettURL = "http://localhost:9117"
	}

	jackettAPIKey := os.Getenv("JACKETT_API_KEY")
	if jackettAPIKey == "" {
		log.Fatal("❌ JACKETT_API_KEY environment variable is required")
	}

	tmdbAPIKey := os.Getenv("TMDB_API_KEY")
	if tmdbAPIKey == "" {
		log.Fatal("❌ TMDB_API_KEY environment variable is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("✅ Port: %s\n", port)

	// Get cache configuration from environment variables
	searchTTL := getEnvDuration("CACHE_SEARCH_TTL", 30*time.Minute)
	metadataTTL := getEnvDuration("CACHE_METADATA_TTL", 24*time.Hour)
	torboxTTL := getEnvDuration("CACHE_TORBOX_CHECK_TTL", 10*time.Minute)

	fmt.Println()

	// Create addon
	fmt.Println("🔧 Initializing addon...")
	addon := NewTorBoxStremioAddon(torboxAPIKey, jackettURL, jackettAPIKey, tmdbAPIKey, searchTTL, metadataTTL, torboxTTL)
	fmt.Println("✅ Addon initialized")
	fmt.Println()

	// Setup HTTP server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      addon,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	fmt.Println("===========================================")
	fmt.Println("  🚀 Server Started")
	fmt.Println("===========================================")
	fmt.Printf("📝 Manifest:      http://localhost:%s/manifest.json\n", port)
	fmt.Printf("🎬 Movie Test:   http://localhost:%s/stream/movie/tt0111161.json\n", port)
	fmt.Printf("📺 Series Test:  http://localhost:%s/stream/series/tt0903747:1:1.json\n", port)
	fmt.Println("===========================================")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop the server")
	fmt.Println()
	// Start server
	log.Printf("Listening on port %s...", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("❌ Server failed: %v", err)
	}
}
