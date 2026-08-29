package utils_test

import (
	"stremfy/utils"
	"testing"
)

func TestTitleMatcher_Matches(t *testing.T) {
	tm := utils.NewTitleMatcher()

	tests := []struct {
		name        string
		search      string
		alternative string
		collection  string
		rawTorrent  string
		want        bool
	}{
		{
			name:       "Standard 1080p release with dots",
			search:     "Breaking Bad",
			rawTorrent: "Breaking.Bad.S01E01.1080p.BluRay.x265-RARBG",
			want:       true,
		},
		{
			name:       "Portuguese title with accents",
			search:     "A Grande Família",
			rawTorrent: "A.Grande.Familia.2014.Nacional.1080p.WEB-DL",
			want:       true,
		},
		{
			name:       "Portuguese release with accents",
			search:     "A Grande Família",
			rawTorrent: "LAPUMiA.Org - A Grande Família - Pack (720p)",
			want:       true,
		},
		{
			name:        "Portuguese release of international movie",
			search:      "Fight Club",
			alternative: "Clube da Luta",
			rawTorrent:  "Clube Da Luta 1999 1080p AMZN WEB-DL DDP5.1 H.264 DUAL-MADRUGA",
			want:        true,
		},
		{
			name:       "Website tag prefix",
			search:     "Inception",
			rawTorrent: "[vacatorrent.com] Inception 2010 Dual Audio 1080p",
			want:       true,
		},
		{
			name:       "Complex messy release",
			search:     "Harley Quinn",
			rawTorrent: "VACATORRENT.COM.1A-TEMPORADA-COMPLETA-WEB-DL.MKV.-LEGENDADO-..Harley.Quinn.S01.1080p.DCU.WEBRip.DD5.1.x264-NTb[rartv]",
			want:       true,
		},
		{
			name:       "Different movie should not match",
			search:     "Avatar",
			rawTorrent: "Avatar The Way of Water 2022 2160p UHD",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tm.Matches(tt.search, tt.collection, tt.rawTorrent, tt.alternative)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v (raw: %s)", got, tt.want, tt.rawTorrent)
			}
		})
	}
}
