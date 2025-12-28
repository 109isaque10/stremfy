# Build stage
FROM golang:1.25.5 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /docker-gs-ping

# Expose the default port
EXPOSE 8080

# Run the application
CMD ["./stremfy"]
