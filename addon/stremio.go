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
	"stremfy/utils"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"
)

type StremfyAddon struct {
	addon            *stream.Addon
	torboxClient     *debrid.Client
	metadataProvider *metadata.Provider
	matcher          *utils.TitleMatcher
	cache            *caching.CacheInstance
	backgroundWorker *caching.BackgroundWork
	timeLogging      bool
}

func NewStremfyAddon(env EnvConfig, ttl TTLConfig) *StremfyAddon {
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
		Cache:        caching.C().Cache,
	})

	jackettScraper := scrapers.NewJackettScraper(env.JackettURL, env.JackettAPIKey, ttl.CacheSearchTTL, env.JackettEnabled)
	torrProxyScraper := scrapers.NewTorrProxyScraper(env.TorrProxyURL, ttl.CacheSearchTTL, env.TorrProxyEnabled)
	types.Scrapers = append(types.Scrapers, jackettScraper)
	types.Scrapers = append(types.Scrapers, torrProxyScraper)

	var metadataProvider *metadata.Provider
	metadataProvider = metadata.NewMetadataProvider(env.TMDBAPIKey, env.Country, caching.C().Cache)
	logger.Debug("✅ TMDB metadata provider initialized")

	ta := &StremfyAddon{
		addon:            addon,
		torboxClient:     torboxClient,
		metadataProvider: metadataProvider,
		cache:            caching.C(),
		matcher:          utils.NewTitleMatcher(),
	}

	// Initialize background worker with injected dependencies
	ta.backgroundWorker = caching.NewBackgroundWorker(
		ta.metadataProvider,
	)

	timeEnv, timeExists := os.LookupEnv("TIME_LOGGING")
	if timeExists {
		ta.timeLogging, _ = strconv.ParseBool(timeEnv)
	}

	addon.SetStreamHandler(ta.handleStream)

	return ta
}

func (ta *StremfyAddon) handleStream(req stream.StreamRequest) *stream.StreamResponse {
	logger := zap.L()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTime := time.Now()

	logger.Info("📺 Stream request", zap.String("id", req.ID), zap.String("type", req.Type))

	// Build search query
	searchQuery := ta.buildSearchQuery(req)

	req.Title = searchQuery.Title
	req.Year = searchQuery.Year
	req.AlternativeTitle = searchQuery.AlternativeTitle

	// Search items
	items := ta.Search(ctx, searchQuery)
	if items == nil {
		return &stream.StreamResponse{Streams: []stream.Stream{}}
	}

	logger.Info(fmt.Sprintf("🔍 Found %d items", len(items)))

	if len(items) == 0 {
		return &stream.StreamResponse{Streams: []stream.Stream{}}
	}

	// Extract hashes and check TorBox cache
	streams := ta.checkCacheAndBuildStreams(items, req)
	if streams == nil {
		return &stream.StreamResponse{Streams: []stream.Stream{}}
	}

	endTime := time.Since(startTime)
	logger.Info(fmt.Sprintf("⏱ Took %d seconds to fetch!", int(endTime.Seconds())))

	logger.Info(fmt.Sprintf("✅ Returning %d cached streams", len(streams)))

	slices.SortFunc(streams, func(a, b stream.Stream) int {
		return cmp.Compare(b.BehaviorHints.VideoSize, a.BehaviorHints.VideoSize)
	})

	ta.backgroundWorker.UserBackgroundTask(req)

	return &stream.StreamResponse{
		Streams: streams,
	}
}

func (ta *StremfyAddon) buildSearchQuery(req stream.StreamRequest) types.ScrapeRequest {
	scrapeReq := types.ScrapeRequest{
		Title:       ta.getTitleFromIMDb(req.ID),
		MediaType:   req.Type,
		MediaOnlyID: req.ID,
	}

	if req.IsSeries() {
		meta := ta.getMetaFromIMDb(req.ID)
		scrapeReq.Season = req.Season
		episode := req.Episode
		scrapeReq.Episode = &episode
		strID, _ := strconv.Atoi(meta.ID)
		scrapeReq.AlternativeTitle = ta.getTranslatedTitleFromTMDB(strID)
	}

	if req.IsMovie() {
		meta := ta.getMetaFromIMDb(req.ID)
		scrapeReq.Collection = meta.Collection
		scrapeReq.Year = meta.Year
		strID, _ := strconv.Atoi(meta.ID)
		scrapeReq.AlternativeTitle = ta.getAlternativeTitleFromTMDB(strID)
	}

	return scrapeReq
}

func (ta *StremfyAddon) Search(ctx context.Context, query types.ScrapeRequest) []types.ScrapeResult {
	zap.L().Info(fmt.Sprintf("Started search for %s", query.Title))
	// Create a torrent manager with TorBox integration
	torrentMgr := torrentManager.NewTorrentManager(ta.torboxClient)
	// Create channels to receive results
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []types.ScrapeResult

	// Search all scrapers
	for _, idx := range types.Scrapers {
		wg.Add(1)
		go func(idx types.Scraper) {
			defer wg.Done()
			if idx.IsEnabled() {
				results, err := idx.Scrape(ctx, query, torrentMgr)
				if err != nil {
					zap.L().Error(fmt.Sprintf("%s search failed", idx.Name()), zap.Error(err))
					return
				}
				zap.L().Info(fmt.Sprintf("✅ %s returned %d results", idx.Name(), len(results)))
				mu.Lock()
				allResults = append(allResults, results...)
				mu.Unlock()
			}
		}(idx)
	}

	wg.Wait()
	return allResults
}

func (ta *StremfyAddon) checkCacheAndBuildStreams(items []types.ScrapeResult, req stream.StreamRequest) []stream.Stream {
	logger := zap.L()
	// Extract unique hashes
	hashMap := make(map[string]types.ScrapeResult)
	var hashes, webHashes []string

	startTime := time.Now()

	logger.Info("📦 Processing items: ")

	for _, item := range items {
		if item.Hash != "" {
			if _, exists := hashMap[item.Hash]; !exists {
				hashMap[item.Hash] = item
				hashes = append(hashes, item.Hash)
			}
		} else if item.Type == types.DDL {
			hash := ta.torboxClient.HashLink(item.URL)
			if _, exists := hashMap[hash]; !exists {
				item.Hash = hash
				hashMap[hash] = item
				webHashes = append(webHashes, hash)
			}
		}
	}

	if len(hashes) == 0 && len(webHashes) == 0 {
		return []stream.Stream{}
	}

	hashesTime := time.Since(startTime)
	startTime = time.Now()

	logger.Debug(fmt.Sprintf("🔎 Checking %d hashes and %d webHashes in TorBox cache", len(hashes), len(webHashes)))

	_, torboxExists := os.LookupEnv("DISABLE_TORBOX")
	var cached []debrid.CacheCheck
	// Check cache with TorBox
	if torboxExists {
		cached = []debrid.CacheCheck{}
	} else {
		var err error
		cached, err = ta.torboxClient.CheckCache(hashes, types.TORRENT)
		if err != nil {
			logger.Error("torbox cache check failed", zap.Error(err))
			return nil
		}
		cachedLinks, err := ta.torboxClient.CheckCache(webHashes, types.DDL)
		if err != nil {
			logger.Error("torbox web cache check failed", zap.Error(err))
			return nil
		}
		cached = append(cached, cachedLinks...)
	}

	checkCacheTime := time.Since(startTime)
	startTime = time.Now()

	// Build streams from cached results with file filtering
	var streams []stream.Stream
	isSeries := req.IsSeries()

	var wg sync.WaitGroup
	var mu sync.Mutex // To protect streams slice

	for _, cachedItem := range cached {
		hash := cachedItem.Hash
		if hash == "" {
			continue
		}

		// Get original item info
		item, exists := hashMap[hash]
		if !exists {
			continue
		}

		logger.Debug("✅ Cached item", zap.String("type", string(item.Type)), zap.String("title", item.Title), zap.String("hash", hash))

		wg.Add(1)

		startCachedTime := time.Now()
		go func(item types.ScrapeResult, hash string) {
			defer wg.Done()
			startCachedTime := time.Now()

			var files, ID = []debrid.CachedFileInfo{}, ""
			var err error

			// Get file list for the cached torrent
			logger.Debug("📂 Fetching files for cached item", zap.String("hash", hash), zap.String("title", item.Title))
			files, ID, err = ta.torboxClient.FetchFiles(hash, item.URL, item.Type)

			// 2. Single unified fallback handler
			if err != nil {
				logger.Warn("Failed to get files, using fallback", zap.String("hash", hash), zap.Error(err))

				streamed := ta.buildStream(item, req)

				mu.Lock()
				streams = append(streams, streamed)
				mu.Unlock()
				return
			}

			getFilesTime := time.Since(startCachedTime)
			if ta.timeLogging {
				logger.Debug(fmt.Sprintf("---TIME---> GetFilesTime %dms", getFilesTime.Milliseconds()))
			}
			startCachedTime = time.Now()

			logger.Debug(fmt.Sprintf("Found %d files in item", len(files)), zap.String("ID", ID), zap.String("hash", hash), zap.String("title", item.Title))

			var episodeList []debrid.EpisodeInfo
			cached := false // for not setting permanent cache if we got episode info from cache, to avoid unnecessary writes
			if ta.cache != nil && isSeries {
				// Try to get episode info from cache
				cacheKey := fmt.Sprintf("episodeInfo:%s", hash)
				if cachedEpisodeInfo := ta.cache.Cache.Get(cacheKey); cachedEpisodeInfo != nil {
					episodeList = cachedEpisodeInfo.Value().([]debrid.EpisodeInfo)
					cached = true
					logger.Debug("✅ Found episode info in cache", zap.String("itemID", ID), zap.String("hash", hash), zap.String("title", item.Title))
				} else {
					episodeList = make([]debrid.EpisodeInfo, len(files))
				}
			}

			for i, file := range files {
				checkSingleFileStart := time.Now()
				//logger.Debug("🔍 Applying filters to file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("size", debrid.FormatBytes(file.Size)), zap.String("itemID", itemID), zap.String("hash", hash), zap.String("title", torrent.Title))

				// Filter 1: Must be a video file
				if !debrid.IsVideoFile(file.Name) {
					logger.Debug("⏭️ Skipping non-video file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("itemID", ID), zap.String("hash", hash), zap.String("title", item.Title))
					continue
				}

				// Filter 2: Must meet minimum size requirements
				if !debrid.IsFileSizeValid(file.Size, isSeries) {
					logger.Debug("⏭️ Skipping file too small", zap.String("size", debrid.FormatBytes(file.Size)), zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("itemID", ID), zap.String("hash", hash), zap.String("title", item.Title))
					continue
				}

				// Filter 3: For series, must match episode pattern
				var episode debrid.EpisodeInfo
				if episodeList != nil {
					// Check if we already have episode info for this file
					if ep := episodeList[i]; ep.Season != 0 && ep.Episode != 0 {
						episode = ep
					} else {
						// If not in cache, analyze filename
						episode = debrid.IsEpisodeFile(hash, file.Name)
						// Store in episode list for caching
						episodeList[i] = episode
					}
				}

				if isSeries && !(episode.Season == req.Season && episode.Episode == req.Episode) {
					logger.Debug("⏭️ Skipping nonEpisode file", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("itemID", ID), zap.String("hash", hash), zap.String("title", item.Title))
					continue
				}

				if !isSeries && !ta.matcher.MovieMatch(req.Title, file.Name, req.ID, req.Year, req.AlternativeTitle) {
					logger.Debug("⏭️ Skipping wrong movie", zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("itemID", ID), zap.String("hash", hash), zap.String("title", item.Title))
					continue
				}

				logger.Debug("✅ Valid file", zap.String("size", debrid.FormatBytes(file.Size)), zap.String("fileName", file.Name), zap.Int("fileID", file.Index), zap.String("itemID", ID), zap.String("hash", hash), zap.String("title", item.Title))

				// Build stream with URL from requestdl
				streamed := ta.buildStreamWithURL(item, file, ID, req)
				mu.Lock()
				streams = append(streams, streamed)
				mu.Unlock()
				if ta.timeLogging {
					checkSingleFileTime := time.Since(checkSingleFileStart)
					logger.Debug(fmt.Sprintf("---TIME---> CheckSingleFileTime %dms", checkSingleFileTime.Milliseconds()))
				}
			}

			// Cache episode info for series if we have it
			if ta.cache != nil && isSeries && !cached {
				// Cache episode info for series
				cacheKey := fmt.Sprintf("episodeInfo:%s", hash)
				ta.cache.Cache.Set(cacheKey, episodeList, ttlcache.NoTTL)
			}
		}(item, hash)

		if ta.timeLogging {
			checkFilesTime := time.Since(startCachedTime)
			logger.Debug(fmt.Sprintf("---CACHED-> %s", cachedItem.Hash))
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

func (ta *StremfyAddon) buildStreamWithURL(item types.ScrapeResult, file debrid.CachedFileInfo, itemID string, req stream.StreamRequest) stream.Stream {
	// Format title with quality and source info
	title := ta.formatStreamTitleWithFile(item, file)

	// Build file ID for download
	fileID := fmt.Sprintf("%s,%d", itemID, file.Index)

	var downloadURL = ""
	var err error

	// Get download URL from TorBox
	downloadURL, err = ta.torboxClient.UnrestrictLink(fileID, item.Type)

	if err != nil {
		zap.L().Error("Failed to get download link, falling back to InfoHash", zap.String("fileName", file.Name), zap.Error(err))
		// Fallback to InfoHash method
		return stream.Stream{
			InfoHash:    item.Hash,
			FileIdx:     file.Index,
			Description: title,
			Name:        "TorBox",
			Sources:     item.Sources,
			BehaviorHints: &stream.StreamBehaviorHints{
				BingeGroup:  ta.getBingeGroup(req) + item.Hash,
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
			BingeGroup:  ta.getBingeGroup(req) + item.Hash,
			VideoSize:   file.Size,
			Filename:    file.Name,
			NotWebReady: false,
		},
	}
}

func (ta *StremfyAddon) buildStream(item types.ScrapeResult, req stream.StreamRequest) stream.Stream {
	// Format title with quality and source info
	title := ta.formatStreamTitle(item, req)

	// Determine file index
	fileIdx := 0
	if item.FileIndex != nil {
		fileIdx = *item.FileIndex
	}

	streamed := stream.Stream{
		InfoHash:    item.Hash,
		FileIdx:     fileIdx,
		Description: title,
		Name:        "TorBox",
		Sources:     item.Sources,
		BehaviorHints: &stream.StreamBehaviorHints{
			BingeGroup:  ta.getBingeGroup(req) + item.Hash,
			VideoSize:   item.Size,
			Filename:    item.Title,
			NotWebReady: true,
		},
	}

	return streamed
}
