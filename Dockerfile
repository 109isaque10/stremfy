# Build stage
FROM golang:1.25.5 AS builder

WORKDIR /app

# Copy all source code (including vendored dependencies)
COPY . .

# Build the application using vendored dependencies (no network required)
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -a -installsuffix cgo -ldflags="-w -s" -o stremfy .

# Final stage - use distroless for minimal image
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
