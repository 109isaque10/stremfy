package caching

import (
	"context"
	"fmt"
	"strconv"
	"stremfy/metadata"
	"stremfy/stream"
	"stremfy/types"
	"sync"
	"time"

	"go.uber.org/zap"
)

type BackgroundTask struct {
	Type         string // "series-prefetch", "movie-prefetch", "trending-prefetch"
	ID           string
	IMDbID       string
	Title        string
	Year         string
	TotalSeasons int
	Priority     int // 0 = user-triggered (high), 1 = trending (low)
}

type BackgroundWork struct {
	backgroundQueue  chan BackgroundTask
	bgWorkers        int
	taskDeduplicator *TaskDeduplicator
	searchTorrents   types.SearchFunc
	metadataProvider *metadata.Provider
	stopChan         chan struct{}
	workersDone      sync.WaitGroup
}

func NewBackgroundWorker(searchFunc types.SearchFunc, provider *metadata.Provider) *BackgroundWork {
	bk := &BackgroundWork{
		backgroundQueue:  make(chan BackgroundTask, 50),
		bgWorkers:        1,
		taskDeduplicator: NewTaskDeduplicator(),
		searchTorrents:   searchFunc,
		metadataProvider: provider,
		stopChan:         make(chan struct{}),
	}

	bk.startBackgroundWorkers()
	bk.startTrending()

	return bk
}

// startBackgroundWorkers starts goroutines to process background tasks
func (bk *BackgroundWork) startBackgroundWorkers() {
	for i := 0; i < bk.bgWorkers; i++ {
		bk.workersDone.Add(1)
		go bk.backgroundWorker(i)
	}
	zap.L().Debug(fmt.Sprintf("🔧 Started %d background workers for cache warming", bk.bgWorkers))
}

func (bk *BackgroundWork) Stop() {
	zap.L().Debug("🛑 Stopping background workers...")

	// Signal all workers to stop
	close(bk.stopChan)

	// Close the queue (workers will finish current tasks)
	close(bk.backgroundQueue)

	// Wait for all workers to finish with timeout
	done := make(chan struct{})
	go func() {
		bk.workersDone.Wait()
		close(done)
	}()

	select {
	case <-done:
		zap.L().Debug("✅ All background workers stopped gracefully")
	case <-time.After(30 * time.Second):
		zap.L().Debug("⚠️ Background workers did not stop within timeout")
	}
}

// StopAndWait stops workers and waits indefinitely
func (bk *BackgroundWork) StopAndWait() {
	zap.L().Debug("🛑 Stopping background workers...")
	close(bk.stopChan)
	close(bk.backgroundQueue)
	bk.workersDone.Wait()
	zap.L().Debug("✅ All background workers stopped")
}

// TaskDeduplicator prevents duplicate tasks from being queued
type TaskDeduplicator struct {
	mu      sync.RWMutex
	pending map[string]time.Time // IMDbID -> queued time
}

func NewTaskDeduplicator() *TaskDeduplicator {
	td := &TaskDeduplicator{
		pending: make(map[string]time.Time),
	}

	// Cleanup old entries every hour
	go td.cleanupLoop()

	return td
}

func (td *TaskDeduplicator) ShouldQueue(id string, maxAge time.Duration) bool {
	td.mu.Lock()
	defer td.mu.Unlock()

	if queuedAt, exists := td.pending[id]; exists {
		// If queued recently (within maxAge), skip
		if time.Since(queuedAt) < maxAge {
			return false
		}
	}

	td.pending[id] = time.Now()
	return true
}

func (td *TaskDeduplicator) Remove(imdbID string) {
	td.mu.Lock()
	defer td.mu.Unlock()
	delete(td.pending, imdbID)
}

func (td *TaskDeduplicator) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		td.mu.Lock()
		now := time.Now()
		for imdbID, queuedAt := range td.pending {
			// Remove entries older than 24 hours
			if now.Sub(queuedAt) > 24*time.Hour {
				delete(td.pending, imdbID)
			}
		}
		td.mu.Unlock()
	}
}

// backgroundWorker processes tasks with priority
func (bk *BackgroundWork) backgroundWorker(workerID int) {
	defer bk.workersDone.Done()

	zap.L().Debug(fmt.Sprintf("🔧 [Worker %d] Started", workerID))

	for {
		select {
		case task, ok := <-bk.backgroundQueue:
			if !ok {
				// Channel closed, exit
				zap.L().Debug(fmt.Sprintf("🛑 [Worker %d] Queue closed, exiting", workerID))
				return
			}

			zap.L().Debug(fmt.Sprintf("🔄 [Worker %d] Starting", workerID), zap.String("type", task.Type), zap.String("title", task.Title),
				zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))

			switch task.Type {
			case "series-prefetch":
				bk.prefetchSeriesSeasons(task)
			case "movie-prefetch":
				bk.prefetchMovie(task)
			case "trending-prefetch":
				bk.prefetchTrendingContent()
			}

			// Mark task as completed
			bk.taskDeduplicator.Remove(task.ID)

			zap.L().Debug(fmt.Sprintf("✅ [Worker %d] Completed", workerID), zap.String("type", task.Type), zap.String("title", task.Title),
				zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))

		case <-bk.stopChan:
			// Stop signal received, exit gracefully
			zap.L().Debug(fmt.Sprintf("🛑 [Worker %d] Stop signal received, exiting", workerID))
			return
		}
	}
}

func (bk *BackgroundWork) UserBackgroundTask(req stream.StreamRequest) {
	// === BACKGROUND:  Queue prefetch task (non-blocking) ===
	if req.IsSeries() {
		meta, err := bk.metadataProvider.GetMetadataFromTMDB(req.ID)
		fullMetadata, err := bk.metadataProvider.GetTVShowDetails(meta.ID)
		if err == nil && meta != nil {
			// Check if already queued recently (within 24 hours)
			if bk.taskDeduplicator.ShouldQueue(meta.ID, 24*time.Hour) {
				select {
				case bk.backgroundQueue <- BackgroundTask{
					Type:         "series-prefetch",
					IMDbID:       req.ID,
					ID:           meta.ID,
					Title:        fullMetadata.Name,
					Year:         fullMetadata.Year,
					TotalSeasons: fullMetadata.NumberOfSeasons,
					Priority:     0, // High priority (user-triggered)
				}:
					zap.L().Debug("📋 Queued background prefetch", zap.String("type", meta.Type), zap.String("title", meta.Title),
						zap.String("id", meta.ID), zap.String("year", meta.Year))
				default:
					zap.L().Warn("Background queue full")
				}
			} else {
				zap.L().Debug("⏭️ Skipping prefetch (already queued recently)", zap.String("type", meta.Type), zap.String("title", meta.Title),
					zap.String("id", meta.ID), zap.String("year", meta.Year))
			}
		}
	}
}

// prefetchSeriesSeasons downloads hashes for all seasons/episodes
func (bk *BackgroundWork) prefetchSeriesSeasons(task BackgroundTask) {
	// Use a longer timeout for background tasks
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	zap.L().Debug("🎬 Prefetching all seasons", zap.String("type", task.Type), zap.String("title", task.Title),
		zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))

	// Search for complete series
	queries := []string{
		fmt.Sprintf("%s complet", task.Title),
	}

	// Also search season by season
	for season := 1; season <= task.TotalSeasons; season++ {
		queries = append(queries, fmt.Sprintf("%s S%02d", task.Title, season))
	}

	var allHashes []string
	var mu sync.Mutex

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // Max 5 concurrent searches

	for _, query := range queries {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			searchReq := types.ScrapeRequest{
				Title:       query,
				MediaType:   "movie",
				MediaOnlyID: task.IMDbID,
			}

			torrents := bk.searchTorrents(ctx, searchReq)

			// Extract hashes (this downloads . torrent files and caches them)
			for _, torrent := range torrents {
				if torrent.InfoHash != "" {
					mu.Lock()
					allHashes = append(allHashes, torrent.InfoHash)
					mu.Unlock()
				}
			}

			zap.L().Debug(fmt.Sprintf("📦 Background:  Found %d torrents", len(torrents)), zap.String("query", q), zap.String("type", task.Type), zap.String("title", task.Title),
				zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))
		}(query)
	}

	wg.Wait()

	// Deduplicate hashes
	uniqueHashes := make(map[string]bool)
	for _, hash := range allHashes {
		uniqueHashes[hash] = true
	}

	zap.L().Debug(fmt.Sprintf("✅ Prefetch complete:  Downloaded and cached %d unique torrent hashes", len(uniqueHashes)), zap.String("type", task.Type), zap.String("title", task.Title),
		zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))
}

// prefetchMovieVariants downloads hashes for different quality variants
func (bk *BackgroundWork) prefetchMovie(task BackgroundTask) {
	logger := zap.L()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	logger.Debug("🎬 Prefetching movie", zap.String("type", task.Type), zap.String("title", task.Title),
		zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))

	// Search with different quality keywords
	queries := []string{
		fmt.Sprintf("%s %s", task.Title, task.Year),
	}

	var allHashes []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, query := range queries {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()

			searchReq := types.ScrapeRequest{
				Title:       q,
				MediaType:   "movie",
				MediaOnlyID: task.IMDbID,
			}

			torrents := bk.searchTorrents(ctx, searchReq)
			if torrents == nil {
				logger.Error("Background search failed", zap.String("query", q), zap.String("type", task.Type), zap.String("title", task.Title),
					zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))
				return
			}

			for _, torrent := range torrents {
				if torrent.InfoHash != "" {
					mu.Lock()
					allHashes = append(allHashes, torrent.InfoHash)
					mu.Unlock()
				}
			}

			logger.Debug(fmt.Sprintf("📦 Background: Found %d torrents", len(torrents)), zap.String("query", q), zap.String("type", task.Type), zap.String("title", task.Title),
				zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))
		}(query)
	}

	wg.Wait()

	// Deduplicate
	uniqueHashes := make(map[string]bool)
	for _, hash := range allHashes {
		uniqueHashes[hash] = true
	}

	logger.Debug(fmt.Sprintf("✅ Prefetch complete:  Downloaded and cached %d unique torrent hashes", len(uniqueHashes)), zap.String("type", task.Type), zap.String("title", task.Title),
		zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))
}

func (bk *BackgroundWork) startTrending() {
	zap.L().Debug("🎬 Starting trending content prefetcher")
	checkInterval := 12 * time.Hour

	// Run immediately on startup
	go bk.prefetchTrendingContent()

	// Then run every checkInterval
	ticker := time.NewTicker(checkInterval)
	go func() {
		for range ticker.C {
			bk.prefetchTrendingContent()
		}
	}()
}

func (bk *BackgroundWork) prefetchTrendingContent() {
	logger := zap.L()

	logger.Debug("📊 Checking for trending content to prefetch...")

	// Only prefetch if queue is mostly empty (idle)
	if len(bk.backgroundQueue) > 10 {
		logger.Debug("⏭️ Background queue not idle, skipping trending prefetch")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	//Fetch trending movies and TV shows
	//trendingMovies, err := bk.metadataProvider.FetchTrendingMovies(ctx)
	//if err != nil {
	//	logger.Error("Failed to fetch trending movies", zap.Error(err))
	//	return
	//}

	trendingTV, err := bk.metadataProvider.FetchTrendingTV(ctx)
	if err != nil {
		logger.Error("Failed to fetch trending TV shows", zap.Error(err))
		return
	}

	// Combine and limit to top 40
	var allTrending []metadata.TMDBTrendingItem
	//allTrending = append(allTrending, trendingMovies...)
	allTrending = append(allTrending, trendingTV...)

	// Limit to 40 items
	maxItems := 40
	if len(allTrending) > maxItems {
		allTrending = allTrending[:maxItems]
	}

	logger.Debug(fmt.Sprintf("🎯 Found %d trending items to prefetch", len(allTrending)))

	// Queue prefetch tasks for each trending item
	queued := 0
	for _, item := range allTrending {

		// Check deduplication (24 hours for trending)
		if !bk.taskDeduplicator.ShouldQueue(strconv.Itoa(item.ID), 24*time.Hour) {
			logger.Debug("⏭️ Skipping (already prefetched)", zap.String("type", item.MediaType), zap.String("title", item.Title),
				zap.Int("IMDbID", item.ID), zap.Int("totalSeasons", item.TotalSeasons))
			continue
		}

		var year string
		switch item.MediaType {
		case "movie":
			// Extract year from release date (format: YYYY-MM-DD)
			if item.ReleaseDate != "" && len(item.ReleaseDate) >= 4 {
				year = item.ReleaseDate[:4]
			}
			break
		case "tv":
			if item.FirstAirDate != "" && len(item.FirstAirDate) >= 4 {
				year = item.FirstAirDate[:4]
			}
			item.Title = item.Name
		}

		imdbID, _ := bk.metadataProvider.GetIMDbID(ctx, item.MediaType, strconv.Itoa(item.ID))

		// Queue the task
		task := BackgroundTask{
			ID:       strconv.Itoa(item.ID),
			IMDbID:   imdbID,
			Title:    item.Title,
			Year:     year,
			Priority: 1, // Low priority (trending)
		}

		if item.MediaType == "tv" {
			task.Type = "series-prefetch"
			task.TotalSeasons = 5 // Prefetch first 5 seasons for trending shows
		} else {
			task.Type = "movie-prefetch"
		}

		select {
		case bk.backgroundQueue <- task:
			queued++
			logger.Debug(fmt.Sprintf("📋 Queued trending prefetch [%d/%d]", queued, len(allTrending)), zap.String("type", task.Type), zap.String("title", task.Title),
				zap.String("id", task.ID), zap.String("year", task.Year), zap.Int("priority", task.Priority), zap.Int("totalSeasons", task.TotalSeasons), zap.String("IMDbID", task.IMDbID))

			// Small delay to avoid overwhelming the system
			time.Sleep(2 * time.Second)

		default:
			logger.Warn(fmt.Sprintf("Queue full, stopping trending prefetch at %d items", queued))
			return
		}

		// Stop if queue is getting full
		if len(bk.backgroundQueue) > 30 {
			logger.Warn(fmt.Sprintf("Queue filling up, pausing trending prefetch at %d items", queued))
			return
		}
	}

	logger.Debug(fmt.Sprintf("✅ Queued %d trending items for prefetch", queued))
}

// GetQueueSize returns current queue size for monitoring
func (bk *BackgroundWork) GetQueueSize() int {
	return len(bk.backgroundQueue)
}

// GetQueueCapacity returns queue capacity
func (bk *BackgroundWork) GetQueueCapacity() int {
	return cap(bk.backgroundQueue)
}
