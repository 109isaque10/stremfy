# Build stage
FROM golang:1.25.5 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /docker-gs-ping

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/stremfy .

# Copy example.env as reference (users should provide their own .env)
COPY example.env .

# Create cache directory (distroless doesn't support RUN, so we'll handle this via volume)

# Expose the default port
EXPOSE 8080

# Run the application
CMD ["./stremfy"]
