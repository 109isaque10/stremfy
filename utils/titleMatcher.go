package utils

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/wasilibs/go-re2"
	"go.uber.org/zap"
)

// TitleMatcher handles title matching with multiple strategies
type TitleMatcher struct {
	minScore int
}

var sRe = re2.MustCompile(`s\d{1,2}`)
var yearRe = re2.MustCompile(`\d{4}`)
var imdbRe = re2.MustCompile(`([tt\d+])`)

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

func NewTitleMatcher(minScore int) *TitleMatcher {
	if minScore == 0 {
		minScore = 70 // Default 70% match
	}
	return &TitleMatcher{minScore: minScore}
}

// Matches checks if torrent title matches search title
func (tm *TitleMatcher) Matches(searchTitle, collectionTitle, torrentTitle string) bool {
	// Strategy 1: Normalized exact/contains match (fast)
	search := tm.normalize(searchTitle)
	collection := tm.normalize(collectionTitle)
	torrent := strings.ToLower(ExtractMainTitle(torrentTitle))
	searchNoArticles := normalizeWhitespace(articlesRe.ReplaceAllString(search, ""))

	if search == torrent {
		zap.L().Debug("Exact match", zap.String("torrent", torrent), zap.String("search", search), zap.String("title", torrentTitle))
		return true
	}

	if collection == torrent {
		zap.L().Debug("Exact match", zap.String("torrent", torrent), zap.String("search", collection), zap.String("title", torrentTitle))
		return true
	}
	zap.L().Debug("collection didnt match", zap.String("collection", collection))

	// Try match without articles
	if searchNoArticles == torrent {
		zap.L().Debug("Match without articles", zap.String("torrent", torrent), zap.String("search", search), zap.String("searchNoArticles", searchNoArticles), zap.String("title", torrentTitle))
		return true
	}

	zap.L().Debug("Unmatched", zap.String("torrent", torrent), zap.String("search", search), zap.String("collection", collection), zap.String("title", torrentTitle))

	return false
}

func (tm *TitleMatcher) normalize(title string) string {
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

func (tm *TitleMatcher) wordMatchScore(search, torrent string) int {
	searchWords := strings.Fields(search)
	year := parseInt(searchWords[len(searchWords)-1])
	torrentWords := strings.Fields(torrent)

	if len(searchWords) == 0 {
		return 0
	}

	matchCount := 0
	for _, sw := range searchWords {
		for _, tw := range torrentWords {
			// Exact word match or one contains the other (for variations)
			if sw == tw || strings.Contains(tw, sw) || strings.Contains(sw, tw) || (sw == strconv.Itoa(year) && strings.Contains(sw, strconv.Itoa(year+1)) && strings.Contains(sw, strconv.Itoa(year-1))) {
				matchCount++
				break
			}
		}
	}

	return (matchCount * 100) / len(searchWords)
}

func (tm *TitleMatcher) regexMatch(searchTitle, torrentTitle string) bool {
	normalized := tm.normalize(searchTitle)
	words := strings.Fields(normalized)

	if len(words) == 0 {
		return false
	}

	// Build flexible pattern
	pattern := "(?i)"
	for i, word := range words {
		pattern += re2.QuoteMeta(word)
		if i < len(words)-1 {
			pattern += `[.\s\-_:]*`
		}
	}

	regex, err := re2.Compile(pattern)
	if err != nil {
		return false
	}

	return regex.MatchString(torrentTitle)
}

func (tm *TitleMatcher) MovieMatch(searchTitle, fileTitle, imdbId, year, alternativeTitle string) bool {
	if imdbRe.FindString(fileTitle) == imdbId {
		zap.L().Debug("Match by id", zap.String("title", fileTitle))
		return true
	} else if yearRe.FindString(fileTitle) == year {
		zap.L().Debug("Match by year", zap.String("title", fileTitle))
		return true
	}

	// Strategy 1: Normalized exact/contains match (fast)
	search := tm.normalize(searchTitle)
	alternative := tm.normalize(alternativeTitle)
	file := strings.ToLower(ExtractMainTitle(fileTitle))
	searchNoArticles := normalizeWhitespace(articlesRe.ReplaceAllString(search, ""))

	if search == file {
		zap.L().Debug("Exact match", zap.String("file", file), zap.String("search", search), zap.String("title", fileTitle))
		return true
	}

	if alternative == file {
		zap.L().Debug("Exact match", zap.String("file", file), zap.String("search", alternative), zap.String("title", fileTitle))
		return true
	}

	// Try match without articles
	if searchNoArticles == file {
		zap.L().Debug("Match without articles", zap.String("file", file), zap.String("search", search), zap.String("searchNoArticles", searchNoArticles), zap.String("title", fileTitle))
		return true
	}

	zap.L().Debug("Unmatched", zap.String("file", file), zap.String("search", search), zap.String("alternative", alternative), zap.String("title", fileTitle))

	return false
}
