package addon

import (
	"fmt"
	"os"
	"strconv"
	"stremfy/debrid"
	"stremfy/metadata"
	"stremfy/stream"
	"stremfy/types"
	"stremfy/utils"
	"strings"
	"time"

	"go.uber.org/zap"
)

func (ta *StremfyAddon) getBingeGroup(req stream.StreamRequest) string {
	if req.IsSeries() {
		return fmt.Sprintf("torbox|%s|", req.ID)
	}
	return fmt.Sprintf("torbox|%s|", req.ID)
}

func (ta *StremfyAddon) getTitleFromIMDb(imdbID string) string {
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

func (ta *StremfyAddon) getMetaFromIMDb(imdbID string) *metadata.CachedMetadata {
	// Try to get from TMDB if available
	if ta.metadataProvider != nil {
		meta, err := ta.metadataProvider.GetMetadataFromTMDB(imdbID)
		if err == nil && meta != nil {
			return meta
		}
		zap.L().Error("Failed to get meta from TMDB (using IMDb ID)", zap.String("IMDbID", imdbID), zap.Error(err))
	} else {
		zap.L().Warn("Metadata provider not configured, using IMDb ID", zap.String("IMDbID", imdbID))
	}

	// Fallback to IMDb ID
	return nil
}

func (ta *StremfyAddon) getAlternativeTitleFromTMDB(ID int) string {
	// Try to get from TMDB if available
	if ta.metadataProvider != nil {
		title, err := ta.metadataProvider.GetAlternativeTitleFromTMDB(ID)
		if err == nil && title != "" {
			return title
		} else if err != nil && strings.Contains(err.Error(), "no title returned") || err == nil && title == "" {
			zap.L().Warn("No titles returned for selected country from TMDb", zap.Int("ID", ID), zap.Error(err))
		} else {
			zap.L().Error("Failed to get alternative title from TMDB (using TMDB ID)", zap.Int("ID", ID), zap.Error(err))
		}
	} else {
		zap.L().Warn("Metadata provider not configured, using TMDB ID", zap.Int("ID", ID))
	}

	return ""
}

func (ta *StremfyAddon) getTranslatedTitleFromTMDB(ID int) string {
	// Try to get from TMDB if available
	if ta.metadataProvider != nil {
		title, err := ta.metadataProvider.GetTranslatedTitleFromTMDB(ID)
		if err == nil && title != "" {
			return title
		} else if err != nil && strings.Contains(err.Error(), "no title returned") || err == nil && title == "" {
			zap.L().Warn("No titles returned for selected country from TMDb", zap.Int("ID", ID), zap.Error(err))
		} else {
			zap.L().Error("Failed to get translated title from TMDB (using TMDB ID)", zap.Int("ID", ID), zap.Error(err))
		}
	} else {
		zap.L().Warn("Metadata provider not configured, using TMDB ID", zap.Int("ID", ID))
	}

	return ""
}

func (ta *StremfyAddon) formatStreamTitle(torrent types.ScrapeResult, req stream.StreamRequest) string {
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

func (ta *StremfyAddon) formatStreamTitleWithFile(torrent types.ScrapeResult, file debrid.CachedFileInfo) string {
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
