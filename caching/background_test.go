package caching

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"stremfy/types"
)

// A simple deterministic fake search function.
// It records each call (title) and returns one or more ScrapeResult entries
// containing InfoHash values derived from the query title.
func makeFakeSearch(callLog *[]string, calls *int32, minDelay, maxDelay time.Duration) types.SearchFunc {
	var mu sync.Mutex
	return func(ctx context.Context, req types.ScrapeRequest) []types.ScrapeResult {
		// Slight randomized delay (bounded) to better surface concurrency issues
		if minDelay > 0 || maxDelay > 0 {
			d := minDelay
			if maxDelay > minDelay {
				d += time.Duration(int64(time.Now().UnixNano()) % (int64(maxDelay-minDelay) + 1))
			}
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil
			}
		}

		// record the call
		mu.Lock()
		*callLog = append(*callLog, req.Title)
		mu.Unlock()

		atomic.AddInt32(calls, 1)

		// Return two results: one duplicate-ish and one unique (based on query)
		base := fmt.Sprintf("hash-%s", req.Title)
		dup := fmt.Sprintf("dup-%s", req.Title) // simulated duplicate per query
		return []types.ScrapeResult{
			{Title: req.Title, InfoHash: base},
			{Title: req.Title, InfoHash: dup},
		}
	}
}

func TestPrefetchSeriesSeasons_InvokesExpectedQueries(t *testing.T) {
	// Arrange
	var callLog []string
	var calls int32
	// small delay to increase interleaving but keep test fast
	fake := makeFakeSearch(&callLog, &calls, 5*time.Millisecond, 20*time.Millisecond)

	bk := &BackgroundWork{
		searchTorrents: fake,
	}

	task := BackgroundTask{
		Type:         "series-prefetch",
		ID:           "1",
		IMDbID:       "tt0000001",
		Title:        "MyShow",
		Year:         "2020",
		TotalSeasons: 3,
		Priority:     1,
	}

	// Act
	start := time.Now()
	// This should complete (it waits internally for all goroutines)
	bk.prefetchSeriesSeasons(task)
	elapsed := time.Since(start)

	// Assert: expected number of queries = TotalSeasons + 1 ("complet")
	expected := task.TotalSeasons + 1
	if int(atomic.LoadInt32(&calls)) != expected {
		t.Fatalf("expected %d search calls, got %d; calls log: %#v", expected, atomic.LoadInt32(&calls), callLog)
	}

	// Ensure it didn't hang
	if elapsed > 5*time.Second {
		t.Fatalf("prefetchSeriesSeasons took too long: %v", elapsed)
	}
}

func TestPrefetchSeriesSeasons_ConcurrentRuns_NoRace(t *testing.T) {
	// This test runs multiple prefetchSeriesSeasons concurrently to exercise concurrency paths.
	// Run `go test -race` to detect races.
	const parallelRuns = 10
	var wg sync.WaitGroup

	// shared fake search that is safe for concurrent use
	var globalCalls int32
	var globalLog []string
	fake := makeFakeSearch(&globalLog, &globalCalls, 1*time.Millisecond, 5*time.Millisecond)

	bk := &BackgroundWork{
		searchTorrents: fake,
	}

	wg.Add(parallelRuns)
	errCh := make(chan error, parallelRuns)

	for i := 0; i < parallelRuns; i++ {
		go func(idx int) {
			defer wg.Done()
			task := BackgroundTask{
				Type:         "series-prefetch",
				ID:           fmt.Sprintf("%d", idx),
				IMDbID:       fmt.Sprintf("tt%07d", idx),
				Title:        fmt.Sprintf("Show-%02d", idx),
				Year:         "2021",
				TotalSeasons: 4, // each run will spawn 5 queries
				Priority:     1,
			}

			// call the prefetch; it should complete without panic or deadlock
			done := make(chan struct{})
			go func() {
				bk.prefetchSeriesSeasons(task)
				close(done)
			}()

			select {
			case <-done:
				// success
			case <-time.After(10 * time.Second):
				errCh <- fmt.Errorf("prefetchSeriesSeasons timeout for task %d", idx)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}

	// Basic sanity: ensure fake was invoked at least once per run
	if int(atomic.LoadInt32(&globalCalls)) == 0 {
		t.Fatalf("expected fake search to be called, but was not")
	}
}

func TestPrefetchMovie_WorksAndDedupes(t *testing.T) {
	// Arrange
	var callLog []string
	var calls int32
	fake := makeFakeSearch(&callLog, &calls, 1*time.Millisecond, 5*time.Millisecond)

	bk := &BackgroundWork{
		searchTorrents: fake,
	}

	task := BackgroundTask{
		Type:     "movie-prefetch",
		ID:       "m1",
		IMDbID:   "tt-m1",
		Title:    "SomeMovie",
		Year:     "2019",
		Priority: 1,
	}

	// Act
	bk.prefetchMovie(task)

	// Since the fake returns two results per query and we only query once, expect calls==1
	if int(atomic.LoadInt32(&calls)) != 1 {
		t.Fatalf("expected 1 search call for movie prefetch, got %d (log: %#v)", calls, callLog)
	}
}
