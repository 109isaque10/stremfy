# Stremfy

A Stremio addon that integrates TorBox and Jackett for searching and streaming torrents.

## Features

- Search torrents via Jackett
- Stream with TorBox debrid service
- TMDB metadata integration
- Caching for improved performance
- Docker support with auto-building to GitHub Container Registry

## Quick Start with Docker

### Using Docker Compose (Recommended)

1. Clone the repository:
```bash
git clone https://github.com/109isaque10/stremfy.git
cd stremfy
```

2. Create a `.env` file with your API keys:
```bash
cp example.env .env
# Edit .env with your actual API keys
```

3. Start the service:
```bash
docker-compose up -d
```

4. Access the addon at `http://localhost:8080/manifest.json`

### Using Pre-built Docker Image

Pull and run the latest image from GitHub Container Registry:

```bash
docker pull ghcr.io/109isaque10/stremfy:latest

docker run -d \
  --name stremfy \
  -p 8080:8080 \
  -e TORBOX_API_KEY=your_torbox_api_key \
  -e JACKETT_URL=http://your-jackett-url:9117 \
  -e JACKETT_API_KEY=your_jackett_api_key \
  -e TMDB_API_KEY=your_tmdb_api_key \
  -v ./cache:/app/cache \
  ghcr.io/109isaque10/stremfy:latest
```

### Building Locally

```bash
docker build -t stremfy:local .
```

## Configuration

All configuration is done via environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `TORBOX_API_KEY` | Your TorBox API key | (required) |
| `JACKETT_URL` | Jackett server URL | http://localhost:9117 |
| `JACKETT_API_KEY` | Your Jackett API key | (required) |
| `TMDB_API_KEY` | Your TMDB API key | (required) |
| `PORT` | Server port | 8080 |
| `CACHE_SEARCH_TTL` | Search cache TTL (minutes) | 30 |
| `CACHE_METADATA_TTL` | Metadata cache TTL (minutes) | 1440 |
| `CACHE_TORBOX_CHECK_TTL` | TorBox check cache TTL (minutes) | 10 |

## Development

### Prerequisites

- Go 1.25.5 or later
- TorBox API key
- Jackett instance
- TMDB API key

### Running Locally

1. Install dependencies:
```bash
go mod download
```

2. Create `.env` file:
```bash
cp example.env .env
# Edit .env with your API keys
```

3. Run the application:
```bash
go run main.go
```

### Testing Endpoints

- Manifest: `http://localhost:8080/manifest.json`
- Movie Test: `http://localhost:8080/stream/movie/tt0111161.json`
- Series Test: `http://localhost:8080/stream/series/tt0903747:1:1.json`

## Docker Image

The Docker image is automatically built and pushed to GitHub Container Registry on every commit to the main branch and on version tags.

Available tags:
- `latest` - Latest build from main branch
- `v1.0.0` - Specific version tags
- `main` - Latest build from main branch

## License

See LICENSE file for details.
