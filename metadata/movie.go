package metadata

type TMDBMovie struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	ReleaseDate   string `json:"release_date"`
}

type BelongsToCollection struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TMDB API response structures
type TMDBCollectionResponse struct {
	BelongsToCollection *BelongsToCollection `json:"belongs_to_collection"`
}

type TMDBAlternativeTitlesResponse struct {
	Titles []AlternativeTitle `json:"titles"`
}

type AlternativeTitle struct {
	Title string `json:"title"`
}
