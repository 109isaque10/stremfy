package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Helper to decode JSON body into a map
func decodeBody(t *testing.T, rr *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	if rr.Code >= 400 {
		t.Fatalf("unexpected status code: %d, body: %s", rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(out); err != nil {
		t.Fatalf("failed to decode response JSON: %v, body: %s", err, rr.Body.String())
	}
}

func TestAddon_ManifestEndpoint(t *testing.T) {
	man := Manifest{
		ID:      "test.addon",
		Version: "0.1.0",
		Name:    "Test Addon",
	}
	addon := NewAddon(man)

	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	rr := httptest.NewRecorder()

	addon.ServeHTTP(rr, req)

	var got Manifest
	decodeBody(t, rr, &got)

	if got.Name != man.Name || got.ID != man.ID || got.Version != man.Version {
		t.Fatalf("manifest mismatch: got %+v want %+v", got, man)
	}
}

func TestAddon_StreamEndpoint_MovieAndSeries(t *testing.T) {
	man := Manifest{ID: "s.test", Name: "S", Version: "v"}
	addon := NewAddon(man)

	// Stream handler echoes back the request in streams for inspection
	addon.SetStreamHandler(func(req StreamRequest) *StreamResponse {
		name := "movie"
		if req.IsSeries() {
			name = fmt.Sprintf("series-%s-%d-%d", req.ID, req.Season, req.Episode)
		}
		return &StreamResponse{
			Streams: []Stream{
				{URL: "http://example.com/stream", InfoHash: "hash123", Name: name},
			},
		}
	})

	// Movie request
	reqMovie := httptest.NewRequest(http.MethodGet, "/stream/movie/tt9999999.json", nil)
	rrMovie := httptest.NewRecorder()
	addon.ServeHTTP(rrMovie, reqMovie)

	var srMovie StreamResponse
	decodeBody(t, rrMovie, &srMovie)
	if len(srMovie.Streams) != 1 || srMovie.Streams[0].InfoHash != "hash123" {
		t.Fatalf("unexpected movie stream response: %+v", srMovie)
	}

	// Series request with season/episode
	reqSeries := httptest.NewRequest(http.MethodGet, "/stream/series/tt1111111:2:3.json", nil)
	rrSeries := httptest.NewRecorder()
	addon.ServeHTTP(rrSeries, reqSeries)

	var srSeries StreamResponse
	decodeBody(t, rrSeries, &srSeries)
	if len(srSeries.Streams) != 1 {
		t.Fatalf("unexpected series stream response count: %+v", srSeries)
	}
	// Check the stream Name contains correct parsing
	if srSeries.Streams[0].Name != "series-tt1111111-2-3" {
		t.Fatalf("unexpected stream name for series request: %s", srSeries.Streams[0].Name)
	}
}

func TestParseStreamID_ValidAndInvalid(t *testing.T) {
	tests := []struct {
		id          string
		expectID    string
		expectS     int
		expectE     int
		expectError bool
	}{
		{"tt0000001", "tt0000001", 0, 0, false},
		{"tt0000002:1:1", "tt0000002", 1, 1, false},
		{"tt1234567:10:99", "tt1234567", 10, 99, false},
		{"invalid:1:1", "", 0, 0, true},
		{"tt123:nope:1", "", 0, 0, true},
	}

	for _, tc := range tests {
		imdbID, s, e, err := ParseStreamID(tc.id)
		if tc.expectError {
			if err == nil {
				t.Fatalf("expected error for %q but got none", tc.id)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.id, err)
		}
		if imdbID != tc.expectID || s != tc.expectS || e != tc.expectE {
			t.Fatalf("parse mismatch for %q: got (%s,%d,%d) want (%s,%d,%d)", tc.id, imdbID, s, e, tc.expectID, tc.expectS, tc.expectE)
		}
	}
}

func TestAddon_HandlersNotSet(t *testing.T) {
	man := Manifest{ID: "empty", Name: "Empty", Version: "v"}
	addon := NewAddon(man)

	// Catalog not set
	req := httptest.NewRequest(http.MethodGet, "/catalog/movie/x.json", nil)
	rr := httptest.NewRecorder()
	addon.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for missing catalog handler, got %d", rr.Code)
	}

	// Meta not set
	req2 := httptest.NewRequest(http.MethodGet, "/meta/movie/tt123.json", nil)
	rr2 := httptest.NewRecorder()
	addon.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for missing meta handler, got %d", rr2.Code)
	}

	// Stream not set
	req3 := httptest.NewRequest(http.MethodGet, "/stream/movie/tt123.json", nil)
	rr3 := httptest.NewRecorder()
	addon.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for missing stream handler, got %d", rr3.Code)
	}
}
