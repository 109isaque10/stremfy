package scrapers

import (
	"testing"
)

// TestIsSeasonPack tests the season pack detection function
func TestIsSeasonPack(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected bool
	}{
		// Season range tests
		{
			name:     "Season range S01-S03",
			title:    "Show Name S01-S03 1080p",
			expected: true,
		},
		{
			name:     "Season range S01-03",
			title:    "Show Name S01-03 720p",
			expected: true,
		},
		{
			name:     "Season range uppercase",
			title:    "SHOW NAME S1-S3 COMPLETE",
			expected: true,
		},
		{
			name:     "Season text range",
			title:    "Show Name Season 1-3 Complete",
			expected: true,
		},
		{
			name:     "Portuguese season range",
			title:    "Show Name Temporada 1-3 Completa",
			expected: true,
		},
		
		// Complete series tests
		{
			name:     "Complete series",
			title:    "Show Name Complete Series 1080p",
			expected: true,
		},
		{
			name:     "Complete season",
			title:    "Show Name Complete Season 1",
			expected: true,
		},
		{
			name:     "Full series",
			title:    "Show Name Full Series BluRay",
			expected: true,
		},
		{
			name:     "Full season",
			title:    "Show Name Full Season 2",
			expected: true,
		},
		{
			name:     "Portuguese complete series",
			title:    "Show Name Série Completa 1080p",
			expected: true,
		},
		{
			name:     "Portuguese serie completa",
			title:    "Show Name Serie Completa",
			expected: true,
		},
		{
			name:     "Portuguese temporada completa",
			title:    "Show Name Temporada Completa",
			expected: true,
		},
		
		// Pack tests
		{
			name:     "Season pack with space",
			title:    "Show Name Season Pack 1080p",
			expected: true,
		},
		{
			name:     "Season pack with dot",
			title:    "Show.Name.Season.Pack.720p",
			expected: true,
		},
		{
			name:     "Show pack with space",
			title:    "Show Name Show Pack",
			expected: true,
		},
		{
			name:     "Show pack with dot",
			title:    "Show.Name.Show.Pack",
			expected: true,
		},
		{
			name:     "Portuguese pack completo",
			title:    "Show Name Pack Completo",
			expected: true,
		},
		{
			name:     "Portuguese coleção completa",
			title:    "Show Name Coleção Completa",
			expected: true,
		},
		{
			name:     "Portuguese colecao completa",
			title:    "Show Name Colecao Completa",
			expected: true,
		},
		
		// Should NOT be detected as season pack
		{
			name:     "Single episode",
			title:    "Show Name S01E05 1080p",
			expected: false,
		},
		{
			name:     "Single season",
			title:    "Show Name S01 1080p",
			expected: false,
		},
		{
			name:     "Normal title",
			title:    "Show Name 2024 1080p WEB-DL",
			expected: false,
		},
		{
			name:     "Episode with quality",
			title:    "Show.Name.S02E10.PROPER.1080p",
			expected: false,
		},
		{
			name:     "Movie title",
			title:    "Movie Name 2024 1080p BluRay",
			expected: false,
		},
		{
			name:     "Season indicator without range",
			title:    "Show Name Season 2 Episode 5",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSeasonPack(tt.title)
			if result != tt.expected {
				t.Errorf("isSeasonPack(%q) = %v, expected %v", tt.title, result, tt.expected)
			}
		})
	}
}

// TestIsSeasonPackEdgeCases tests edge cases
func TestIsSeasonPackEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected bool
	}{
		{
			name:     "Empty string",
			title:    "",
			expected: false,
		},
		{
			name:     "Only spaces",
			title:    "   ",
			expected: false,
		},
		{
			name:     "Case insensitive complete series",
			title:    "Show Name COMPLETE SERIES",
			expected: true,
		},
		{
			name:     "Mixed case season pack",
			title:    "Show Name SeAsOn PaCk",
			expected: true,
		},
		{
			name:     "Season range with extra zeros",
			title:    "Show Name S001-S003",
			expected: false, // Our regex expects 1-2 digits
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSeasonPack(tt.title)
			if result != tt.expected {
				t.Errorf("isSeasonPack(%q) = %v, expected %v", tt.title, result, tt.expected)
			}
		})
	}
}
