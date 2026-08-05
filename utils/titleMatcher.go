package utils

import (
	"strings"
	"unicode"

	"github.com/wasilibs/go-re2"
	"go.uber.org/zap"
)

// TitleMatcher handles title matching with multiple strategies
type TitleMatcher struct{}

var sRe = re2.MustCompile(`s\d{1,2}`)
var yearRe = re2.MustCompile(`\d{4}`)
var imdbRe = re2.MustCompile(`tt\d+`)

var titleNormalizer = strings.NewReplacer(
	"&", "and",
	"'", "",
	"complete", "",
	"completo", "",
	"completa", "",
	"s01-", "",
	"•", "",
	"®", "",
	"í", "i",
	"é", "e",
	"ã", "a",
	"ç", "c",
)

func NewTitleMatcher() *TitleMatcher {
	return &TitleMatcher{}
}

// Matches checks if torrent title matches search title
func (tm *TitleMatcher) Matches(searchTitle, collectionTitle, torrentTitle string) bool {
	search := tm.normalize(searchTitle)
	collection := tm.normalize(collectionTitle)
	torrent := strings.ToLower(ExtractMainTitle(torrentTitle))
	searchNoArticles := normalizeWhitespace(articlesRe.ReplaceAllString(search, ""))

	if search == torrent {
		zap.L().Debug("Exact match", zap.String("torrent", torrent), zap.String("search", search), zap.String("title", torrentTitle))
		return true
	}

	// Try match with collection title
	if collection == torrent {
		zap.L().Debug("Exact match", zap.String("torrent", torrent), zap.String("search", collection), zap.String("title", torrentTitle))
		return true
	}

	// Try match without articles
	if searchNoArticles == torrent {
		zap.L().Debug("Match without articles", zap.String("torrent", torrent), zap.String("search", search), zap.String("searchNoArticles", searchNoArticles), zap.String("title", torrentTitle))
		return true
	}

	zap.L().Debug("Unmatched", zap.String("torrent", torrent), zap.String("search", search), zap.String("collection", collection), zap.String("title", torrentTitle))

	return false
}

func (tm *TitleMatcher) normalize(title string) string {
	if title == "" {
		return title
	}

	title = strings.ToLower(title)
	title = titleNormalizer.Replace(title)
	title = sRe.ReplaceAllString(title, "")
	title = strings.ReplaceAll(title, "collection", "")

	// Remove punctuation except spaces
	var result strings.Builder
	result.Grow(len(title)) // Pre-allocate

	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			result.WriteRune(r)
		} else {
			result.WriteRune(' ')
		}
	}

	return normalizeWhitespace(result.String())
}

func (tm *TitleMatcher) MovieMatch(searchTitle, fileTitle, imdbId, year, alternativeTitle string) bool {
	// Try simpler matchs first
	if imdbRe.FindString(fileTitle) == imdbId {
		zap.L().Debug("Match by IMDb id", zap.String("title", fileTitle))
		return true
	}

	search := tm.normalize(searchTitle)
	alternative := tm.normalize(alternativeTitle)
	file := strings.ToLower(ExtractMainTitle(fileTitle))
	searchNoArticles := normalizeWhitespace(articlesRe.ReplaceAllString(search, ""))

	if search == file {
		zap.L().Debug("Exact match", zap.String("file", file), zap.String("search", search), zap.String("title", fileTitle))
		return true
	}

	// Try match with translated title
	if alternative == file {
		zap.L().Debug("Exact match", zap.String("file", file), zap.String("search", alternative), zap.String("title", fileTitle))
		return true
	}

	// Try match without articles
	if searchNoArticles == file {
		zap.L().Debug("Match without articles", zap.String("file", file), zap.String("search", search), zap.String("searchNoArticles", searchNoArticles), zap.String("title", fileTitle))
		return true
	}

	// Substring Title Match + Matching Year (Prevents wrong movie in same-year packs)
	if yearRe.FindString(strings.ToLower(fileTitle)) == year {
		if strings.Contains(file, search) || (alternative != "" && strings.Contains(file, alternative)) {
			zap.L().Debug("Similar match with year", zap.String("file", file), zap.String("search", search), zap.String("alternative", alternative), zap.String("title", fileTitle), zap.String("year", year))
			return true
		}
	}

	zap.L().Debug("Unmatched", zap.String("file", file), zap.String("search", search), zap.String("alternative", alternative), zap.String("title", fileTitle))
	return false
}
