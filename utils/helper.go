package utils

import (
	"strings"

	"github.com/wasilibs/go-re2"
)

type infoTable = []struct {
	keywords []string
	label    string
}

type keywordMatcher struct {
	re    *re2.Regexp
	label string
}

var qualities = infoTable{
	{[]string{"2160p", "4k", "uhd"}, "4K"},
	{[]string{"1080p", "fhd"}, "1080p"},
	{[]string{"720p", "hd"}, "720p"},
	{[]string{"480p"}, "480p"},
}

var codecs = infoTable{
	{[]string{"h265", "hevc", "x265"}, "H265"},
	{[]string{"h264", "x264", "avc"}, "H264"},
	{[]string{"av1"}, "AV1"},
	{[]string{"xvid"}, "XviD"},
}

var sources = infoTable{
	{[]string{"bluray", "blu-ray", "bdrip", "bd-rip", "brrip", "br-rip"}, "Source"},
	{[]string{"webdl", "web-dl", "dvdrip", "dvd-rip", "webrip", "web-rip", "dvd"}, "Premium"},
	{[]string{"screener", "scr", "tvrip", "tv-rip", "hdtv", "pdtv"}, "Standard"},
	{[]string{"cam", "camrip", "cam-rip", "telesync", "ts", "workprint", "wp"}, "Poor"},
}

var (
	qualityMatchers = compileTable(qualities)
	codecMatchers   = compileTable(codecs)
	sourceMatchers  = compileTable(sources)
)

func keywordRegex(kw string) *re2.Regexp {
	var b strings.Builder
	b.WriteString(`(?:^|[^a-z0-9])`) // left boundary (RE2 has no lookarounds)
	for i, r := range strings.ToLower(kw) {
		if i > 0 {
			b.WriteString(`[.\-_ ]*`)
		}
		b.WriteString(re2.QuoteMeta(string(r)))
	}
	b.WriteString(`(?:[^a-z0-9]|$)`) // right boundary
	return re2.MustCompile(b.String())
}

func compileTable(info infoTable) []keywordMatcher {
	var matchers []keywordMatcher
	for _, row := range info {
		for _, kw := range row.keywords {
			matchers = append(matchers, keywordMatcher{re: keywordRegex(kw), label: row.label})
		}
	}
	return matchers
}

func matchTable(title string, matchers []keywordMatcher) string {
	for _, m := range matchers {
		if m.re.MatchString(title) {
			return m.label
		}
	}

	return "Unknown"
}

func ExtractInfo(title string) (string, string, string) {
	title = strings.ToLower(title)
	return matchTable(title, qualityMatchers), matchTable(title, codecMatchers), matchTable(title, sourceMatchers)
}
