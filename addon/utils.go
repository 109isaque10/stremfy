package addon

import (
	"fmt"
	"os"
	"strconv"
	"stremfy/debrid"
	"stremfy/stream"
	"stremfy/types"
	"stremfy/utils"
	"strings"
	"time"

	"go.uber.org/zap"
)

func (ta *TorBoxStremioAddon) getBingeGroup(req stream.StreamRequest) string {
	if req.IsSeries() {
		return fmt.Sprintf("torbox|%s|", req.ID)
	}
	return fmt.Sprintf("torbox|%s|", req.ID)
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
