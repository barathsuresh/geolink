# docker/api.Dockerfile
# Multi-stage build for the GEOLINK API server binary.
#
# Stage 1 (builder): compiles cmd/api/main.go into a statically-linked binary.
# Stage 2 (runtime): minimal scratch image — no shell, no libc, minimal attack surface.

# ─── Stage 1: Build ───────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# Install git (required for some Go module fetches)
RUN apk add --no-cache git

WORKDIR /app

# Cache dependency downloads as a separate layer
COPY go.mod go.sum ./
RUN go mod download

# Copy source and compile
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /geolink-api ./cmd/api

# ─── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /geolink-api /geolink-api

EXPOSE 8080

ENTRYPOINT ["/geolink-api"]
