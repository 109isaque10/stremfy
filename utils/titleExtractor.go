package utils

import (
	"strings"

	"github.com/coregx/coregex"
)

// Title-case sequence regex (greedy). Will match "The Walking Dead The Ones Who Live"
// Allows small stopwords between TitleCase words.
var titleCaseRe = coregex.MustCompile(`([A-Z][a-z0-9'’&]*(?:[ ._\-:]+(?:(?:of|the|and|for|an|in|on|to|with|without)|[A-Z][a-z0-9'’&]*))*)`)

// trailerRe locates the first common "trailer" token that usually follows the title.
var trailerRe = coregex.MustCompile(`(?i)\b(?:S\d{1,2}(?:E\d{1,2})?|E\d{2}|\d{3,4}p|720p|1080p|2160p|4k|web(?:-?dl)?|amzn|ddp\d(?:\.\d+)?|dd[45]|webrip|dvdrip|bdrip|bluray|hdrip|h264|x264|x265|hevc|dual|dub(?:lado)?|legendad(?:o)?|dublado|rartv|glhf|rip|ts)\b|(?:\(|\[)|\bcam\b`)

// domainLike detects tokens that look like a domain (vacatorrent.com) or similar
var domainLike = coregex.MustCompile(`(?i)^[a-z0-9]+(?:\.[a-z0-9]+)+$`)
var allUpper = coregex.MustCompile(`^[A-Z0-9\-_]{2,}$`)

// packSuffixRe strips trailing pack/complete words and any following tokens.
// Examples matched: "completo", "completa", "complete", "full", "pack", "complete series", "parte"
var packSuffixRe = coregex.MustCompile(`(?i)\b(?:completo|completa|complete(?:\s+series)?|full(?:\s+series)?|pack|parte|todas\s+as\s+temporadas|all\s+seasons|temporada(?:\s+parte)?(?:\s*\d+)?)\b.*$`)

// stopWordRe matches small words that commonly introduce subtitles or pack markers
var stopWordRe = coregex.MustCompile(`(?i)\b(?:a|o|the|an|el|la|de|da|dos|das|temporada|parte|season)\b`)

// articleFollowedByCapRe finds an article followed by a capitalized token (likely start of subtitle)
var articleFollowedByCapRe = coregex.MustCompile(`(?i)\b(?:a|o|the|an|el|la|de|da|dos|das)\b\s+\p{Lu}`)

// small set of known common noise tokens (lowercase)
var shortNoise = map[string]bool{
	"mp4": true, "720p": true, "1080p": true, "web": true, "webrip": true, "web-dl": true,
	"webdl": true, "x264": true, "h264": true, "x265": true, "hevc": true, "dxva": true,
	"aac": true, "ddp": true, "dual": true, "dublado": true, "legendado": true,
	"eng": true, "brazilian": true, "rip": true,
}

// Add these at the top with other regex variables (after line 27 in title_extractor4.go)
var (
	nonAlphaRe   = coregex.MustCompile(`^[^A-Za-z0-9]+$`)
	seasonEpRe   = coregex.MustCompile(`(?i)^(temporada|completa?|season|s\d+)$`)
	resolutionRe = coregex.MustCompile(`(?i)^(\d{3,4}p?|hdr|uhd|4k|2160p|1080p|720p|480p)$`)
	fileSizeRe   = coregex.MustCompile(`(?i)^(\d+(\.\d+)?|gb|mb|kb)$`)
	formatRe     = coregex.MustCompile(`(?i)^(mkv|mp4|avi|wmv|mov|flv)$`)
	sxxRe        = coregex.MustCompile(`(?i)^s\d{1,2}(?:e\d{1,2})?$`)
	yearRe       = coregex.MustCompile(`^\d{4}$`)
	articlesRe   = coregex.MustCompile(`\b(a|the|o|filme|serie|série|show)\b`)
)

// ExtractMainTitle attempts to return only the main title phrase from a noisy release string.
func ExtractMainTitle(raw string) string {
	if raw == "" {
		return ""
	}

	// Normalize separators so regex sees words rather than dots/underscores
	var normalizer = strings.NewReplacer(
		".", " ",
		"_", " ",
		"/", " ",
		"-", " ",
		":", " ",
	)

	s := normalizeWhitespace(normalizer.Replace(raw))

	// Split to tokens and skip leading noise tokens (domains, all-uppercase group tokens, short noise)
	// Replace lines 60-77 (the skip logic)
	// Split to tokens and skip leading noise tokens
	words := strings.Fields(s)
	skip := 0
	maxSkip := 25 // Increased from 6 to handle longer noise prefixes

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

	// 1) Try TitleCase matches on the cleaned string and pick the longest match
	allMatches := titleCaseRe.FindAllStringSubmatchIndex(clean, -1)
	if len(allMatches) > 0 {
		bestStart := -1
		bestCandidate := ""
		for _, idxs := range allMatches {
			if len(idxs) < 4 {
				continue
			}
			groupStart := idxs[2]
			groupEnd := idxs[3]
			if groupStart < 0 || groupEnd < 0 || groupEnd <= groupStart {
				continue
			}
			candidate := strings.TrimSpace(clean[groupStart:groupEnd])

			// apply trailer trim and pack suffix removal early to evaluate candidate length/position properly
			if tidx := trailerRe.FindStringIndex(candidate); tidx != nil {
				candidate = strings.TrimSpace(candidate[:tidx[0]])
			}
			candidate = stripPackSuffix(candidate)

			// Skip if empty after trimming
			if candidate == "" {
				continue
			}

			// Selection logic:
			// 1. Prefer matches that start at position 0 (beginning of clean string)
			// 2. Among position-0 matches, prefer the longest one
			// 3. If no position-0 matches, take the earliest one
			if groupStart == 0 {
				// Match starts at beginning - prefer longer matches
				if bestStart != 0 || len(candidate) > len(bestCandidate) {
					bestCandidate = candidate
					bestStart = groupStart
				}
			} else if bestStart != 0 {
				// No position-0 match yet, so prefer earliest match
				if bestStart == -1 || groupStart < bestStart {
					bestCandidate = candidate
					bestStart = groupStart
				}
			}
		}

		if bestCandidate != "" {
			// After selecting the earliest TitleCase candidate, truncate at stop words (articles introducing subtitles)
			bestCandidate = truncateAtStopWord(bestCandidate)
			return normalizeWhitespace(bestCandidate)
		}
	}

	// 2) Fallback: chop off everything starting at the first trailer token
	end := len(clean)
	if idx := trailerRe.FindStringIndex(clean); idx != nil {
		end = idx[0]
	}
	prefix := strings.TrimSpace(clean[:end])
	if prefix == "" {
		return ""
	}

	// Take contiguous run until we hit a year/season token or trailer token
	pWords := strings.Fields(prefix)
	endIdx := 0
	sxxRe := coregex.MustCompile(`(?i)^s\d{1,2}(?:e\d{1,2})?$`)
	xxRe := coregex.MustCompile(`^\d{4}$`)
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
		return result
	}

	result := normalizeWhitespace(strings.Join(pWords[:endIdx], " "))
	result = truncateAtStopWord(result)
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
	// Remove trailing pack-like suffixes (in-place)
	return strings.TrimSpace(packSuffixRe.ReplaceAllString(s, ""))
}

// truncateAtStopWord truncates candidate when it finds a stop-word (article/temporada/parte)
// that likely indicates the start of a subtitle. It only truncates if the stop-word occurs
// after the first word (so we don't cut titles like "El Camino").
func truncateAtStopWord(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return candidate
	}

	// If candidate has only one word, don't truncate
	words := strings.Fields(candidate)
	if len(words) <= 1 {
		return candidate
	}

	// Look for an article followed by a capitalized token (likely subtitle): " A Breaking Bad Movie"
	// Only search after the first word to preserve titles starting with articles
	afterFirstWord := strings.Join(words[1:], " ")
	if loc := articleFollowedByCapRe.FindStringIndex(afterFirstWord); loc != nil {
		// Found an article+cap pattern
		// Check the article itself - if it's uppercase "A" (not "a"), it's more likely a subtitle
		articleStart := len(words[0]) + 1 + loc[0]
		if articleStart < len(candidate) {
			articleWord := ""
			remaining := candidate[articleStart:]
			if idx := strings.Index(remaining, " "); idx > 0 {
				articleWord = remaining[:idx]
			} else {
				articleWord = remaining
			}

			// If the article is single uppercase "A", it's likely starting a subtitle
			// "El Camino A Breaking Bad Movie" -> "A" starts subtitle
			// vs "Fear the Walking Dead" -> "the" is part of title
			if articleWord == "A" || articleWord == "An" {
				// This is likely a subtitle, truncate here
				firstWord := words[0]
				truncateAt := len(firstWord) + 1 + loc[0]
				return strings.TrimSpace(candidate[:truncateAt])
			}

			// For lowercase articles (the, of, and), check if they connect title parts
			// by looking at the word BEFORE the article
			if loc[0] > 0 {
				beforeArticle := afterFirstWord[:loc[0]]
				beforeWords := strings.Fields(beforeArticle)
				if len(beforeWords) > 0 {
					lastWordBefore := beforeWords[len(beforeWords)-1]
					// If word before article is TitleCase, check following pattern
					if len(lastWordBefore) > 0 && lastWordBefore[0] >= 'A' && lastWordBefore[0] <= 'Z' {
						// Count consecutive TitleCase words after the article
						afterArticlePos := articleStart
						afterArticle := candidate[afterArticlePos:]
						afterWords := strings.Fields(afterArticle)

						if len(afterWords) >= 2 {
							titleCaseCount := 0
							for i := 1; i < len(afterWords) && i < 4; i++ {
								w := afterWords[i]
								if len(w) > 0 && w[0] >= 'A' && w[0] <= 'Z' {
									titleCaseCount++
								} else {
									break
								}
							}

							// If 2+ TitleCase words follow, it's part of the title
							if titleCaseCount >= 2 {
								return candidate
							}
						}
					}
				}
			}
		}

		// Default: truncate before the article
		firstWord := words[0]
		truncateAt := len(firstWord) + 1 + loc[0]
		return strings.TrimSpace(candidate[:truncateAt])
	}

	return candidate
}

func shouldSkipWord(w string) bool {
	lw := strings.ToLower(w)

	// Quick checks first (cheaper operations)
	if len(w) <= 2 && allUpper.MatchString(w) {
		return true
	}

	if shortNoise[lw] || strings.Contains(lw, "vacatorrent") || strings.Contains(lw, "torrent") {
		return true
	}

	// Regex checks (more expensive)
	return domainLike.MatchString(w) ||
		allUpper.MatchString(w) ||
		seasonEpRe.MatchString(w) ||
		resolutionRe.MatchString(w) ||
		fileSizeRe.MatchString(w) ||
		formatRe.MatchString(w) ||
		nonAlphaRe.MatchString(w)
}
