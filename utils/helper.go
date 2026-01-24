package utils

import "strings"

func ExtractQuality(title string) string {
	titleLower := strings.ToLower(title)

	qualities := []struct {
		keywords []string
		label    string
	}{
		{[]string{"2160p", "4k", "uhd"}, "4K"},
		{[]string{"1080p", "fhd"}, "1080p"},
		{[]string{"720p", "hd"}, "720p"},
		{[]string{"480p"}, "480p"},
	}

	for _, q := range qualities {
		for _, kw := range q.keywords {
			if strings.Contains(titleLower, kw) {
				return q.label
			}
		}
	}

	return "Unknown"
}

func ExtractCodec(title string) string {
	titleLower := strings.ToLower(title)

	codecs := []struct {
		keywords []string
		label    string
	}{
		{[]string{"h265", "hevc", "x265"}, "H265"},
		{[]string{"h264", "x264", "avc"}, "H264"},
		{[]string{"av1"}, "AV1"},
		{[]string{"xvid"}, "XviD"},
	}

	for _, c := range codecs {
		for _, kw := range c.keywords {
			if strings.Contains(titleLower, kw) {
				return c.label
			}
		}
	}

	return ""
}

func ExtractSource(title string) string {
	titleLower := strings.ToLower(title)

	codecs := []struct {
		keywords []string
		label    string
	}{
		{[]string{"bluray", "blu-ray", "bdrip", "bd-rip", "brrip", "br-rip"}, "Source"},
		{[]string{"webdl", "web-dl", "dvdrip", "dvd-rip", "webrip", "web-rip", "dvd"}, "Premium"},
		{[]string{"screener", "scr", "tvrip", "tv-rip", "hdtv", "pdtv"}, "Standard"},
		{[]string{"cam", "camrip", "cam-rip", "telesync", "ts", "workprint", "wp"}, "Poor"},
	}

	for _, c := range codecs {
		for _, kw := range c.keywords {
			if strings.Contains(titleLower, kw) {
				return c.label
			}
		}
	}

	return ""
}
