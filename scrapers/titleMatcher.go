package scrapers

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// TitleMatcher handles title matching with multiple strategies
type TitleMatcher struct {
	minScore int
}

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
	torrent := tm.normalize(torrentTitle)

	if search == torrent || strings.Contains(torrent, search) {
		return true
	}

	// Strategy 2: Word-by-word matching
	score := tm.wordMatchScore(search, torrent)
	if score >= tm.minScore {
		return true
	}

	// Strategy 3: Regex pattern matching (fallback)
	if tm.regexMatch(searchTitle, torrentTitle) {
		return true
	}

	return false
}

func (tm *TitleMatcher) normalize(title string) string {
	title = strings.ToLower(title)

	// Remove common articles and words
	replacements := map[string]string{
		" the ": " ", " a ": " ", " an ": " ",
		" o ": " ", " os ": " ", " as ": " ",
		"&": "and", "'s": "", "'": "",
	}

	for old, new := range replacements {
		title = strings.ReplaceAll(title, old, new)
	}

	// Remove punctuation except spaces
	var result strings.Builder
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			result.WriteRune(r)
		} else {
			result.WriteRune(' ')
		}
	}

	// Collapse spaces
	return strings.Join(strings.Fields(result.String()), " ")
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
		pattern += regexp.QuoteMeta(word)
		if i < len(words)-1 {
			pattern += `[.\s\-_:]*`
		}
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}

	return regex.MatchString(torrentTitle)
}
