package addon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type Env struct {
	Key   string
	Value any
}

type EnvConfig struct {
	TorBoxAPIKey     string
	JackettURL       string
	JackettEnabled   bool
	JackettAPIKey    string
	TorrProxyURL     string
	TorrProxyEnabled bool
	TMDBAPIKey       string
	Country          string
	Port             string
}

type TTLConfig struct {
	CacheSearchTTL      time.Duration
	CacheMetadataTTL    time.Duration
	CacheTorBoxCheckTTL time.Duration
}

var EnvDefaults = []Env{
	0: {"TORBOX_API_KEY", ""},
	1: {"JACKETT_URL", "http://localhost:9117"},
	2: {"JACKETT_ENABLED", "false"},
	3: {"JACKETT_API_KEY", ""},
	4: {"TORRPROXY_URL", "http://localhost:3000"},
	5: {"TORRPROXY_ENABLED", "false"},
	6: {"TMDB_API_KEY", ""},
	7: {"COUNTRY", "US"},
	8: {"PORT", "8080"},
}

var TTLDefaults = []Env{
	0: {"CACHE_SEARCH_TTL", 30 * time.Minute},
	1: {"CACHE_METADATA_TTL", 24 * time.Hour},
	2: {"CACHE_TORBOX_CHECK_TTL", 15 * time.Minute},
}

func StartServer() {
	// Force pure Go DNS resolver to avoid CGO overhead
	// This must be set before any network operations
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial:     nil,
	}
	logger := zap.L()
	defer logger.Sync()

	// Get configuration from environment variables
	var env EnvConfig
	for key, envDefault := range EnvDefaults {
		envValue := os.Getenv(envDefault.Key)
		if envValue == "" {
			envValue = fmt.Sprintf("%v", envDefault.Value)
			logger.Debug("Using default value for environment variable", zap.String("key", envDefault.Key), zap.Any("default", envValue))
		} else {
			logger.Debug("Loaded environment variable", zap.String("key", envDefault.Key), zap.String("value", envValue))
		}
		switch key {
		case 0:
			env.TorBoxAPIKey = envValue
		case 1:
			env.JackettURL = envValue
		case 2:
			env.JackettEnabled, _ = strconv.ParseBool(envValue)
		case 3:
			env.JackettAPIKey = envValue
		case 4:
			env.TorrProxyURL = envValue
		case 5:
			env.TorrProxyEnabled, _ = strconv.ParseBool(envValue)
		case 6:
			env.TMDBAPIKey = envValue
		case 7:
			env.Country = envValue
		case 8:
			env.Port = envValue
		}
	}

	var ttl TTLConfig
	for key, ttlDefault := range TTLDefaults {
		ttlValueStr := os.Getenv(ttlDefault.Key)
		var ttlValue time.Duration
		if ttlValueStr == "" {
			ttlValue = ttlDefault.Value.(time.Duration)
			logger.Debug("Using default value for TTL", zap.String("key", ttlDefault.Key), zap.Any("default", ttlValue))
		} else {
			parsed, err := strconv.Atoi(ttlValueStr)
			if err != nil {
				logger.Warn("Invalid TTL value in environment; using default", zap.String("key", ttlDefault.Key), zap.String("value", ttlValueStr), zap.Error(err))
				ttlValue = ttlDefault.Value.(time.Duration)
			} else {
				ttlValue = time.Duration(parsed) * time.Minute
				logger.Debug("Loaded TTL from environment variable", zap.String("key", ttlDefault.Key), zap.String("value", ttlValueStr))
			}
		}
		switch key {
		case 0:
			ttl.CacheSearchTTL = ttlValue
		case 1:
			ttl.CacheMetadataTTL = ttlValue
		case 2:
			ttl.CacheTorBoxCheckTTL = ttlValue
		}
	}

	// Create addon
	logger.Debug("🔧 Initializing addon...")
	addon := NewTorBoxStremioAddon(env, ttl)
	logger.Info("✅ Addon initialized")

	// Setup HTTP server
	server := &http.Server{
		Addr:         ":" + env.Port,
		Handler:      addon,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		sign := <-sigChan
		switch sign {
		case os.Interrupt, syscall.SIGINT, syscall.SIGTERM:
			gracefulShutdown(server, addon)
		case syscall.SIGQUIT:
			quit(server, addon)
		}
	}()

	logger.Info("🚀 Server Started", zap.String("Manifest", fmt.Sprintf("http://localhost:%s/manifest.json", env.Port)),
		zap.String("Movie", fmt.Sprintf("http://localhost:%s/stream/movie/tt0111161.json", env.Port)),
		zap.String("Series", fmt.Sprintf("http://localhost:%s/stream/series/tt0903747:1:1.json", env.Port)))

	// Start server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal("Server failed", zap.Error(err))
	}
}

func (ta *TorBoxStremioAddon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ta.addon.ServeHTTP(w, r)
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
	logger.Sync()

	logger.Info("✅ Graceful shutdown complete")
}

func quit(server *http.Server, addon *TorBoxStremioAddon) {
	logger := zap.L()

	logger.Info("🛑 Starting shutdown...")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	addon.backgroundWorker.Stop()

	// Flush caches to disk
	logger.Debug("💾 Flushing caches to disk...")
	addon.cache.Flush()
	logger.Sync()

	logger.Info("✅ Shutdown complete")
}
