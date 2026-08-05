package utils

import (
	"strings"

	"github.com/wasilibs/go-re2"
)

// Title-case sequence regex (greedy). Will match "The Walking Dead The Ones Who Live"
// Allows small stopwords between TitleCase words.
var titleCaseRe = re2.MustCompile(`(\p{Lu}[\p{L}\-0-9'’&]*(?:[ ._\-:]+(?:(?:of|the|and|for|an|in|on|to|with|without)|\p{Lu}[\p{L}\-0-9'’&]*))*)`)

// trailerRe locates the first common "trailer" token that usually follows the title.
var trailerRe = re2.MustCompile(`(?i)\b(?:S\d{1,2}(?:E\d{1,2})?|E\d{2}|(?:19|20)\d{2}|\d{3,4}p|720p|1080p|2160p|4k|web(?:-?dl)?|amzn|ddp\d(?:\.\d+)?|dd[45]|dts(?:-?hdma)?|webrip|web-rip|dvdrip|bdrip|brrip|bluray|hdrip|h264|h265|repack|x264|x265|hevc|dual|brazilian|dub(?:lado)?|legendad(?:o)?|dublado|rartv|glhf|rip|ts)\b|(?:\(|\[)|\bcam\b`)

// domainLike detects tokens that look like a domain (vacatorrent.com) or similar
var domainLike = re2.MustCompile(`(?i)^[a-z0-9]+(?:\.[a-z0-9]+)+$`)
var allUpper = re2.MustCompile(`^[A-Z0-9\-_]{2,}$`)

// packSuffixRe strips trailing pack/complete words and any following tokens.
// Examples matched: "completo", "completa", "complete", "full", "pack", "complete series", "parte"
var packSuffixRe = re2.MustCompile(`(?i)\s*\b(?:a|o|the|an|el|la|de|da|dos|das)?\s*(\d+[ªº]?\s+)?(?:colecao|completo|completa|collection|complete(?:\s+series)?|full(?:\s+series)?|pack|todas\s+as\s+temporadas|all\s+seasons|temporada(?:\s+parte)?(?:\s*\d+)?|season(?:\s+\d+)?)\b.*$`)

// More aggressive - remove anywhere in string, not just at end
var seasonRangeRe = re2.MustCompile(`(?i)\d+\s*[ªº°]?\s*(?:até|a|to|through|at[eé])\s+\d+\s*[ªº°]?\s*(?:temporada|season).*`)

// stopWordRe matches small words that commonly introduce subtitles or pack markers
// var stopWordRe = re2.MustCompile(`(?i)\b(?:a|o|the|an|el|la|de|da|dos|das|temporada|parte|season)\b`) // used in oldtruncate
var hardStopWordRe = re2.MustCompile(`(?i)\b(?:temporada|season|colecao|collection|completo|completa|complete|full|pack)\b`)

// Match patterns like "8ª", "3ª", "1��", "S08", "Season 3"
var seasonMarkerEndRe = re2.MustCompile(`(?i)\s+\b(\d+[ªº]|s\d+|season)$`)

// articleFollowedByCapRe finds an article followed by a capitalized token (likely start of subtitle)
// var articleFollowedByCapRe = re2.MustCompile(`(?i)\b(?:a|o|the|an|el|la|de|da|dos|das)\b\s+\p{Lu}`) // used only by old truncate

var marketingSuffixRe = re2.MustCompile(`(?i)\b(?:the\s+ultimate|ultimate|collector'?s?\s+edition|special\s+edition|extended\s+edition|anniversary\s+edition)\b.*$`)

var collectionItemRe = re2.MustCompile(`(?i)\bcole[cç][aã]o\b\s*/?\s*\d{1,2}\s*-\s*(.+)$`)

// small set of known common noise tokens (lowercase)
var shortNoise = map[string]bool{
	"mp4": true, "720p": true, "1080p": true, "web": true, "webrip": true, "web-dl": true,
	"webdl": true, "x264": true, "h264": true, "x265": true, "hevc": true, "dxva": true,
	"aac": true, "ddp": true, "dual": true, "dublado": true, "legendado": true,
	"eng": true, "brazilian": true, "rip": true, "collection": true, "coleção": true,
}

var (
	nonAlphaRe = re2.MustCompile(`^[^A-Za-z0-9]+$`)
	fileSizeRe = re2.MustCompile(`(?i)^(\d+(\.\d+)?|gb|mb|kb)$`)
	sxxRe      = re2.MustCompile(`(?i)^s\d{1,2}(?:e\d{1,2})?$`)
	xxRe       = re2.MustCompile(`^\d{4}$`)
	yearEndRe  = re2.MustCompile(`\b(19\d{2}|20\d{2})$`)
	articlesRe = re2.MustCompile(`(?i)\b(a|the|o|filme|serie|série|show)\b`)
)

// Normalize separators so regex sees words rather than dots/underscores
var normalizer = strings.NewReplacer(
	".", " ",
	"_", " ",
	"/", " ",
	"í", "i",
	"é", "e",
	"ã", "a",
	"ç", "c",
	"á", "a",
	"à", "a",
	"â", "a",
	"õ", "o",
	"ô", "o",
	"ú", "u",
	"Í", "I",
	"É", "E",
	"Ã", "A",
	"Ç", "C",
	"Á", "A",
	"À", "A",
	"Â", "A",
	"Ê", "E",
	"Ô", "O",
	"Õ", "O",
	"Ó", "O",
	"Ú", "U",
	// "-", " ",
	// ":", " ",
)

func pickBestTailSegment(raw string) string {
	parts := strings.Split(raw, "/")
	if len(parts) == 0 {
		return raw
	}

	cands := make([]string, 0, 2)
	for i := len(parts) - 1; i >= 0 && len(cands) < 2; i-- {
		p := normalizeWhitespace(strings.TrimSpace(parts[i]))
		if p != "" {
			cands = append(cands, p)
		}
	}
	lCands := len(cands)
	switch lCands {
	case 0:
		return raw
	case 1:
		return cands[0]
	}

	score := func(seg string) int {
		s := normalizer.Replace(seg)
		ws := strings.Fields(s)
		if len(ws) == 0 {
			return -999
		}

		sc := 0
		oneChar := 0
		longWords := 0

		for _, w := range ws {
			rCount := len([]rune(w))
			if rCount == 1 {
				oneChar++
			} else if rCount >= 4 {
				longWords++
			}
			if shouldSkipWord(w) {
				sc -= 2
			}
		}

		sc -= oneChar * 3
		sc += longWords * 2

		if strings.Contains(strings.ToLower(s), " and the ") {
			sc += 3
		}

		return sc
	}

	best := cands[0]
	bestScore := score(best)
	for i := 1; i < len(cands); i++ {
		if sc := score(cands[i]); sc > bestScore {
			best = cands[i]
			bestScore = sc
		}
	}
	return best
}

// ExtractMainTitle attempts to return only the main title phrase from a noisy release string.
func ExtractMainTitle(raw string) string {
	if raw == "" {
		return ""
	}

	if m := collectionItemRe.FindStringSubmatch(raw); len(m) > 1 {
		raw = m[1]
	}

	s := pickBestTailSegment(raw)
	s = normalizeWhitespace(normalizer.Replace(s))

	// Split to tokens and skip leading noise tokens (domains, all-uppercase group tokens, short noise)
	words := strings.Fields(s)
	skip := 0
	maxSkip := 12 // Increased from 6 to handle longer noise prefixes

	for skip < len(words) && skip < maxSkip {
		if !shouldSkipWord(words[skip]) {
			break
		}
		skip++
	}

	// Rebuild string after skipping prefixes
	clean := strings.Join(words[skip:], " ")
	if clean == "" {
		return ""
	}
	if len(clean) < 2 {
		return clean // Too short to process meaningfully
	}

	// 1) Try TitleCase matches on the cleaned string and pick the longest match
	allMatches := titleCaseRe.FindAllStringSubmatchIndex(clean, -1)
	if len(allMatches) > 0 {
		bestStart := -1
		bestCandidate := ""
		bestRawLen := 0

		for _, idxs := range allMatches {
			if len(idxs) < 4 {
				continue
			}

			groupStart := idxs[2]
			groupEnd := idxs[3]

			if groupStart < 0 || groupEnd < 0 || groupEnd <= groupStart {
				continue
			}
			if groupEnd > len(clean) {
				groupEnd = len(clean)
			}
			if groupStart >= groupEnd {
				continue
			}

			// Check if there's a trailer token in the FULL clean string that would cut this match short
			relevantPortion := clean[groupStart:]
			if tidx := trailerRe.FindStringIndex(relevantPortion); tidx != nil {
				// Trailer found in this portion, adjust groupEnd
				newEnd := groupStart + tidx[0]
				if newEnd > groupStart && newEnd <= len(clean) {
					groupEnd = newEnd
				} else {
					continue
				}
			}

			if groupEnd > len(clean) {
				groupEnd = len(clean)
			}

			rawCandidate := strings.TrimSpace(clean[groupStart:groupEnd])
			rawLen := groupEnd - groupStart
			candidate := rawCandidate
			candidate = stripPackSuffix(candidate)

			// Skip if empty after trimming
			if candidate == "" {
				continue
			}

			// Selection logic: prefer earliest match, but if same start position, prefer longest
			shouldUpdate := bestStart == -1 || groupStart < bestStart || (groupStart == bestStart && rawLen > bestRawLen)

			if shouldUpdate {
				bestCandidate = candidate
				bestStart = groupStart
				bestRawLen = rawLen
			}
		}

		if bestCandidate != "" {
			// After selecting the earliest TitleCase candidate, truncate at stop words (articles introducing subtitles)
			bestCandidate = truncateAtStopWord(bestCandidate)
			bestCandidate = strings.ReplaceAll(bestCandidate, ":", " ")
			bestCandidate = stripSeasonMarkers(bestCandidate)
			bestCandidate = stripTrailingYear(bestCandidate)
			bestCandidate = strings.ReplaceAll(bestCandidate, "-", " ")
			bestCandidate = strings.TrimSpace(marketingSuffixRe.ReplaceAllString(bestCandidate, ""))
			return normalizeWhitespace(bestCandidate)
		}
	}

	// 2) Fallback: chop off everything starting at the first trailer token
	end := len(clean)
	if idx := trailerRe.FindStringIndex(clean); idx != nil {
		end = idx[0]
	}
	if end > len(clean) {
		end = len(clean)
	}
	prefix := strings.TrimSpace(clean[:end])
	if prefix == "" {
		return ""
	}

	// Take contiguous run until we hit a year/season token or trailer token
	pWords := strings.Fields(prefix)
	endIdx := 0
	for endIdx < len(pWords) {
		w := pWords[endIdx]
		// stop if token looks like a season/episode or a year-like token
		if sxxRe.MatchString(w) || xxRe.MatchString(w) {
			break
		}
		if trailerRe.MatchString(w) {
			break
		}
		endIdx++
	}

	if endIdx == 0 {
		// Fallback: return first up to 4 words
		limit := 4
		if len(pWords) < limit {
			limit = len(pWords)
		}
		result := normalizeWhitespace(strings.Join(pWords[:limit], " "))
		result = truncateAtStopWord(result)
		result = stripSeasonMarkers(result)
		result = stripTrailingYear(result)
		result = strings.ReplaceAll(result, "-", " ")
		result = strings.TrimSpace(marketingSuffixRe.ReplaceAllString(result, ""))
		return result
	}

	result := normalizeWhitespace(strings.Join(pWords[:endIdx], " "))
	result = truncateAtStopWord(result)
	result = stripSeasonMarkers(result)
	result = stripTrailingYear(result)
	result = strings.ReplaceAll(result, "-", " ")
	return result
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// stripPackSuffix removes trailing pack/completo tokens and anything after them.
func stripPackSuffix(s string) string {
	if s == "" {
		return s
	}
	// Remove season range patterns first (e.g., "1ª até 14ª Temporada")
	s = strings.TrimSpace(seasonRangeRe.ReplaceAllString(s, ""))
	// Remove trailing pack-like suffixes (in-place)
	return strings.TrimSpace(packSuffixRe.ReplaceAllString(s, ""))
}

// Simpler version of truncate
func truncateAtStopWord(candidate string) string {
	candidate = normalizeWhitespace(candidate)
	if candidate == "" {
		return candidate
	}

	words := strings.Fields(candidate)
	if len(words) < 4 {
		return candidate
	}

	// find first HARD stop word index
	cut := -1
	for i, w := range words {
		if hardStopWordRe.MatchString(w) && i >= 2 {
			cut = i
			break
		}
	}
	if cut == -1 {
		return candidate
	}

	// require trailer/noise evidence near cut before truncating
	end := cut + 8
	end = min(end, len(words))

	hasEvidence := false
	for _, w := range words[cut:end] {
		lw := strings.ToLower(strings.Trim(w, "[](){}-_.,"))

		if trailerRe.MatchString(lw) || yearRe.MatchString(lw) || shortNoise[lw] {
			hasEvidence = true
			break
		}
	}

	if !hasEvidence {
		return candidate
	}

	return strings.TrimSpace(strings.Join(words[:cut], " "))
}

// truncateAtStopWord truncates candidate when it finds a stop-word (article/temporada/parte)
// that likely indicates the start of a subtitle. It only truncates if the stop-word occurs
// after the first word (so we don't cut titles like "El Camino").
// oldtruncate, kept for testing
// func oldTruncateAtStopWord(candidate string) string {
// 	candidate = strings.TrimSpace(candidate)
// 	if candidate == "" {
// 		return candidate
// 	}

// 	// If candidate has only one word, don't truncate
// 	words := strings.Fields(candidate)
// 	if len(words) <= 1 {
// 		return candidate
// 	}

// 	// Check if there's a colon pattern in the original candidate
// 	// This indicates an official subtitle (e.g., "Title: Subtitle")
// 	// Common in sequels and franchise movies
// 	if strings.Contains(candidate, ":") || strings.Contains(candidate, "–") || strings.Contains(candidate, "—") {
// 		// Don't truncate - this is likely an official title with subtitle
// 		return candidate
// 	}

// 	// Look for an article followed by a capitalized token
// 	// Only search after the first word to preserve titles starting with articles
// 	afterFirstWord := strings.Join(words[1:], " ")
// 	if loc := articleFollowedByCapRe.FindStringIndex(afterFirstWord); loc != nil {
// 		// Found an article+cap pattern
// 		// Find which word index this corresponds to
// 		articleWordIdx := -1
// 		cumLen := 0
// 		for i := 1; i < len(words); i++ {
// 			if cumLen == loc[0] {
// 				articleWordIdx = i
// 				break
// 			}
// 			cumLen += len(words[i]) + 1 // +1 for space
// 		}

// 		if articleWordIdx == -1 {
// 			// Couldn't find exact word boundary, use old logic
// 			firstWord := words[0]
// 			truncateAt := len(firstWord) + 1 + loc[0]
// 			// Bounds check to prevent panic
// 			if truncateAt > len(candidate) {
// 				truncateAt = len(candidate)
// 			}
// 			if truncateAt < 0 {
// 				return candidate
// 			}
// 			return strings.TrimSpace(candidate[:truncateAt])
// 		}

// 		// Check the article word itself
// 		articleWord := words[articleWordIdx]

// 		// Special case: If the article "A" is the very first word of the candidate,
// 		// it's likely the start of a Portuguese/Spanish title (e.g., "A Grande Família")
// 		// Don't truncate in this case
// 		if (articleWord == "A" || articleWord == "An") && articleWordIdx == 0 {
// 			return candidate // Keep the full title
// 		}

// 		// If the article is uppercase "A" or "An", it's likely starting a subtitle
// 		if articleWord == "A" || articleWord == "An" {
// 			// Truncate before the article
// 			return strings.TrimSpace(strings.Join(words[:articleWordIdx], " "))
// 		}

// 		lastWordBefore := words[articleWordIdx-1]

// 		// If the word before the article is lowercase, it's more likely the article is part of the title
// 		if lastWordBefore == "and" || lastWordBefore == "&" || lastWordBefore == "of" || lastWordBefore == "in" {
// 			return candidate
// 		}

// 		// For lowercase articles (the, of, and, etc), check if followed by 2+ TitleCase words
// 		// This indicates it's part of the title (e.g., "Fear the Walking Dead")
// 		titleCaseCount := 0
// 		for i := articleWordIdx + 1; i < len(words) && i < articleWordIdx+4; i++ {
// 			w := words[i]
// 			if len(w) > 0 && w[0] >= 'A' && w[0] <= 'Z' {
// 				titleCaseCount++
// 			} else {
// 				break
// 			}
// 		}

// 		// If 2+ consecutive TitleCase words follow the article, it's part of the title
// 		if titleCaseCount >= 2 {
// 			return candidate // Don't truncate
// 		}
// 		// Otherwise truncate before the article
// 		return strings.TrimSpace(strings.Join(words[:articleWordIdx], " "))
// 	}

// 	return candidate
// }

func shouldSkipWord(w string) bool {
	lw := strings.ToLower(w)

	// Check for common domain/site indicators first
	if strings.Contains(lw, ".org") || strings.Contains(lw, ".com") ||
		strings.Contains(lw, ".net") || strings.Contains(lw, ".tv") ||
		strings.Contains(lw, "www") || domainLike.MatchString(w) {
		return true
	}

	// All uppercase tokens longer than 2 chars (like release groups "RARBG", "YIFY")
	if len(w) > 2 && allUpper.MatchString(w) {
		return true
	}

	// Very common: short noise words from the map
	if shortNoise[lw] {
		return true
	}

	// Less common: domain-specific checks
	if strings.Contains(lw, "vacatorrent") || strings.Contains(lw, "torrent") {
		return true
	}

	// Remaining regex checks
	return fileSizeRe.MatchString(w) ||
		nonAlphaRe.MatchString(w)
}

func stripTrailingYear(s string) string {
	s = strings.TrimRight(s, "–-—:;,. ")
	s = yearEndRe.ReplaceAllString(s, "")
	return strings.TrimRight(s, "–-—:;,. ")
}

func stripSeasonMarkers(s string) string {
	// Remove trailing season markers like "8ª", "3ª", "S08", etc.
	// Keep removing while pattern matches to handle multiple trailing markers
	s = strings.TrimSpace(s)

	for range 10 {
		if seasonMarkerEndRe.MatchString(s) {
			s = seasonMarkerEndRe.ReplaceAllString(s, "")
		} else {
			break
		}
	}

	// Remove trailing punctuation (–, -, :, etc.)
	s = strings.TrimRight(s, "–-—:;,. ")

	return strings.TrimSpace(s)
}
