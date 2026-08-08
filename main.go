package main

import (
	"encoding/gob"
	"net"
	"stremfy/addon"
	"stremfy/logging"
	"stremfy/metadata"
	"stremfy/types"

	"os"
	"stremfy/debrid"
	"stremfy/scrapers"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"go.uber.org/zap"
	_ "golang.org/x/crypto/x509roots/fallback"
)

func init() {
	// Force pure Go DNS resolver (no CGO)
	net.DefaultResolver.PreferGo = true
	net.DefaultResolver.Dial = nil // Use default dialer
	// Global logger
	infoPath := "logs/info.log"
	debugPath := "logs/debug.log"
	_, debugExists := os.LookupEnv("DEBUG")
	logger, _ := logging.NewMultiSinkLogger(infoPath, debugPath, debugExists)
	zap.ReplaceGlobals(logger)
	// Register all types that will be stored as any in cache
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register([]scrapers.JackettResult{})
	gob.Register(scrapers.JackettResult{})
	gob.Register([]scrapers.TorrProxyResult{})
	gob.Register(scrapers.TorrProxyResult{})
	gob.Register(types.ScrapeResult{})
	gob.Register([]types.ScrapeResult{})
	gob.Register([]string{})
	gob.Register(time.Time{})
	gob.Register(debrid.TorrentInfo{})
	gob.Register(debrid.CachedFileInfo{})
	gob.Register([]debrid.EpisodeInfo{})
	gob.Register(&metadata.CachedMetadata{})
	gob.Register(&metadata.BelongsToCollection{})
}

func main() {
	addon.StartServer()
}
