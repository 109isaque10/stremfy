package addon

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"stremfy/caching"
	"stremfy/debrid"
	"stremfy/metadata"
	"stremfy/scrapers"
	"stremfy/stream"
	"stremfy/torrentManager"
	"stremfy/types"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"
)

type TorBoxStremioAddon struct {
	addon            *stream.Addon
	torboxClient     *debrid.Client
	jackettScraper   *scrapers.JackettScraper
	torrProxyScraper *scrapers.TorrProxyScraper
	metadataProvider *metadata.Provider
	cache            *caching.CacheInstance
	backgroundWorker *caching.BackgroundWork
	timeLogging      bool
}

func NewTorBoxStremioAddon(env EnvConfig, ttl TTLConfig) *TorBoxStremioAddon {
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

	logger := zap.L()

	logger.Debug("✅ Caching system initialized", zap.String("searchTTL", ttl.CacheSearchTTL.String()), zap.String("metadataTTL", ttl.CacheMetadataTTL.String()), zap.String("torboxTTL", ttl.CacheTorBoxCheckTTL.String()))

	torboxClient := debrid.NewClient(debrid.TorboxConfig{
		APIKey:       env.TorBoxAPIKey,
		StoreToCloud: false,
		Timeout:      30 * time.Second,
		CacheTTL:     ttl.CacheTorBoxCheckTTL,
	})

	jackettScraper := scrapers.NewJackettScraper(nil, env.JackettURL, env.JackettAPIKey, ttl.CacheSearchTTL, env.JackettEnabled)
	torrProxyScraper := scrapers.NewTorrProxyScraper(nil, env.TorrProxyURL, ttl.CacheSearchTTL, env.TorrProxyEnabled)
	var metadataProvider *metadata.Provider
	metadataProvider = metadata.NewMetadataProvider(env.TMDBAPIKey, caching.C().Cache)
	logger.Debug("✅ TMDB metadata provider initialized")

	ta := &TorBoxStremioAddon{
		addon:            addon,
		torboxClient:     torboxClient,
		jackettScraper:   jackettScraper,
		torrProxyScraper: torrProxyScraper,
		metadataProvider: metadataProvider,
		cache:            caching.C(),
	}

	// Initialize background worker with injected dependencies
	ta.backgroundWorker = caching.NewBackgroundWorker(
		// Pass searchTorrents as a function
		func(ctx context.Context, req types.ScrapeRequest) []types.ScrapeResult {
			return ta.searchTorrents(ctx, req)
		},
		ta.metadataProvider,
	)

	timeEnv, timeExists := os.LookupEnv("TIME_LOGGING")
	if timeExists {
		ta.timeLogging, _ = strconv.ParseBool(timeEnv)
	}

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
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []types.ScrapeResult
	// Search via Jackett (async)
	if ta.jackettScraper.IsEnabled() {
		wg.Add(1)
		go func(context.Context, types.ScrapeRequest, *torrentManager.TorrentManager) {
			defer wg.Done()
			results, err := ta.jackettScraper.Scrape(ctx, query, torrentMgr)
			if err != nil {
				zap.L().Error("Jackett search failed", zap.Error(err))
				return
			}
			zap.L().Info(fmt.Sprintf("✅ Jackett returned %d results", len(results)))
			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(ctx, query, torrentMgr)
	}
	// Search via TorrProxy (async)
	if ta.torrProxyScraper.IsEnabled() {
		wg.Add(1)
		go func(context.Context, types.ScrapeRequest, *torrentManager.TorrentManager) {
			defer wg.Done()
			results, err := ta.torrProxyScraper.Scrape(ctx, query, torrentMgr)
			if err != nil {
				zap.L().Error("TorrProxy search failed", zap.Error(err))
				return
			}
			zap.L().Info(fmt.Sprintf("✅ TorrProxy returned %d results", len(results)))
			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(ctx, query, torrentMgr)
	}
	wg.Wait()
	return allResults
}

func (ta *TorBoxStremioAddon) checkCacheAndBuildStreams(torrents []types.ScrapeResult, req stream.StreamRequest) []stream.Stream {
	logger := zap.L()
	// Extract unique hashes
	hashMap := make(map[string]types.ScrapeResult)
	var hashes []string

	startTime := time.Now()

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

	hashesTime := time.Since(startTime)
	startTime = time.Now()

	logger.Debug(fmt.Sprintf("🔎 Checking %d hashes in TorBox cache", len(hashes)))

	_, torboxExists := os.LookupEnv("DISABLE_TORBOX")
	var cached []debrid.CacheCheck
	// Check cache with TorBox
	if torboxExists {
		cached = []debrid.CacheCheck{}
	} else {
		var err error
		cached, err = ta.torboxClient.CheckCache(hashes)
		if err != nil {
			logger.Error("torbox cache check failed", zap.Error(err))
			return nil
		}
	}

	checkCacheTime := time.Since(startTime)
	startTime = time.Now()

	// Build streams from cached results with file filtering
	var streams []stream.Stream
	isSeries := req.IsSeries()

	var wg sync.WaitGroup
	var mu sync.Mutex // To protect streams slice

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

		wg.Add(1)

		startCachedTime := time.Now()
		go func(torrent types.ScrapeResult, hash string) {
			defer wg.Done()
			startCachedTime := time.Now()

			// Get file list for the cached torrent
			logger.Debug("📂 Fetching files for cached torrent", zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
			files, torrentID, err := ta.torboxClient.GetTorrentFiles(hash)
			if err != nil {
				logger.Warn("Failed to get files, using fallback", zap.String("hash", hash), zap.Error(err))
				// Fallback to InfoHash method
				streamed := ta.buildStream(torrent, req)
				mu.Lock()
				streams = append(streams, streamed)
				mu.Unlock()
				return
			}

			getFilesTime := time.Since(startCachedTime)
			startCachedTime = time.Now()

			logger.Debug(fmt.Sprintf("Found %d files in torrent", len(files)), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))

			var filesWg sync.WaitGroup
			var episodeList []debrid.EpisodeInfo
			cached := false // for not setting permanent cache if we got episode info from cache, to avoid unnecessary writes
			if ta.cache != nil && isSeries {
				// Try to get episode info from cache
				cacheKey := fmt.Sprintf("episodeInfo:%s", hash)
				if cachedEpisodeInfo := ta.cache.Cache.Get(cacheKey); cachedEpisodeInfo != nil {
					episodeList = cachedEpisodeInfo.Value().([]debrid.EpisodeInfo)
					cached = true
					logger.Debug("✅ Found episode info in cache", zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
				} else {
					episodeList = make([]debrid.EpisodeInfo, len(files))
				}
			}

			for i, file := range files {
				filesWg.Add(1)
				checkSingleFileStart := time.Now()

				go func(file debrid.CachedFileInfo) {
					defer filesWg.Done()
					//logger.Debug("🔍 Applying filters to file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("size", debrid.FormatBytes(file.Size)), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))

					// Filter 1: Must be a video file
					if !debrid.IsVideoFile(file.Name) {
						logger.Debug("⏭️ Skipping non-video file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
						return
					}

					// Filter 2: Must meet minimum size requirements
					if !debrid.IsFileSizeValid(file.Size, isSeries) {
						logger.Debug("⏭️ Skipping file too small", zap.String("size", debrid.FormatBytes(file.Size)), zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
						return
					}

					// Filter 3: For series, must match episode pattern
					var episode debrid.EpisodeInfo
					if episodeList != nil {
						// Check if we already have episode info for this file
						mu.Lock()
						if ep := episodeList[i]; ep.Season != 0 && ep.Episode != 0 {
							mu.Unlock()
							episode = ep
						} else {
							mu.Unlock()
							// If not in cache, analyze filename
							episode = debrid.IsEpisodeFile(hash, file.Name)
							// Store in episode list for caching
							mu.Lock()
							episodeList[i] = episode
							mu.Unlock()
						}
					}

					if isSeries && !(episode.Season == req.Season && episode.Episode == req.Episode) {
						logger.Debug("⏭️ Skipping nonEpisode file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))
						return
					}

					logger.Debug("✅ Valid file", zap.String("size", debrid.FormatBytes(file.Size)), zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("torrentID", torrentID), zap.String("hash", hash), zap.String("torrentTitle", torrent.Title))

					// Build stream with URL from requestdl
					streamed := ta.buildStreamWithURL(torrent, file, torrentID, req)
					mu.Lock()
					streams = append(streams, streamed)
					mu.Unlock()
					if ta.timeLogging {
						checkSingleFileTime := time.Since(checkSingleFileStart)
						logger.Debug(fmt.Sprintf("---TIME---> CheckSingleFileTime %dms", checkSingleFileTime.Milliseconds()))
					}
				}(file)

				if ta.timeLogging {
					logger.Debug(fmt.Sprintf("---TIME---> GetFilesTime %dms", getFilesTime.Milliseconds()))
				}
			}
			filesWg.Wait()

			// Cache episode info for series if we have it
			if ta.cache != nil && isSeries && !cached {
				// Cache episode info for series
				cacheKey := fmt.Sprintf("episodeInfo:%s", hash)
				ta.cache.Cache.Set(cacheKey, episodeList, ttlcache.NoTTL)
			}
		}(torrent, hash)

		if ta.timeLogging {
			checkFilesTime := time.Since(startCachedTime)
			logger.Debug(fmt.Sprintf("---CACHED-> %s", item.Hash))
			logger.Debug(fmt.Sprintf("---TIME---> CheckFilesTime %dms", checkFilesTime.Milliseconds()))
		}
	}
	wg.Wait()

	if ta.timeLogging {
		totalCachedTime := time.Since(startTime)
		logger.Debug(fmt.Sprintf("---TIME---> HashesTime %dms", hashesTime.Milliseconds()))
		logger.Debug(fmt.Sprintf("---TIME---> CheckCacheTime %dms", checkCacheTime.Milliseconds()))
		logger.Debug(fmt.Sprintf("---TIME---> TotalCachedTime %dms", totalCachedTime.Milliseconds()))
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
