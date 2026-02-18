package utils

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/coregx/coregex"
	"go.uber.org/zap"
)

// TitleMatcher handles title matching with multiple strategies
type TitleMatcher struct {
	minScore int
}

var sRe = coregex.MustCompile(`s\d{1,2}`)

var titleNormalizer = strings.NewReplacer(
	"&", "and",
	"'s", "",
	"'", "",
	"complet", "",
	"s01-", "",
)

func NewTitleMatcher(minScore int) *TitleMatcher {
	if minScore == 0 {
		minScore = 70 // Default 70% match
	}
	return &TitleMatcher{minScore: minScore}
}

// Matches checks if torrent title matches search title
func (tm *TitleMatcher) Matches(searchTitle, torrentTitle string) bool {
	// Strategy 1: Normalized exact/contains match (fast)
	search := tm.normalize(searchTitle)
	torrent := strings.ToLower(ExtractMainTitle(torrentTitle))
	searchNoArticles := normalizeWhitespace(articlesRe.ReplaceAllString(search, ""))

	if search == torrent {
		zap.L().Debug("Exact match", zap.String("torrent", torrent), zap.String("search", search), zap.String("title", torrentTitle))
		return true
	}

	// Try match without articles
	if searchNoArticles == torrent {
		zap.L().Debug("Match without articles", zap.String("torrent", torrent), zap.String("search", search), zap.String("searchNoArticles", searchNoArticles), zap.String("title", torrentTitle))
		return true
	}

	zap.L().Debug("Unmatched", zap.String("torrent", torrent), zap.String("search", search), zap.String("title", torrentTitle))

	return false
}

func (tm *TitleMatcher) normalize(title string) string {
	title = strings.ToLower(title)
	title = titleNormalizer.Replace(title)
	title = sRe.ReplaceAllString(title, "")

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
		pattern += coregex.QuoteMeta(word)
		if i < len(words)-1 {
			pattern += `[.\s\-_:]*`
		}
	}

	regex, err := coregex.Compile(pattern)
	if err != nil {
		return false
	}

	return regex.MatchString(torrentTitle)
}
