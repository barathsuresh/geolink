# docker/personalizer.Dockerfile
# Multi-stage build for the search-event personalization consumer.
#
# The personalizer is a long-running service that consumes "search.events"
# and maintains per-IP profiles in Redis.

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /geolink-personalizer ./cmd/personalizer

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /geolink-personalizer /geolink-personalizer

ENTRYPOINT ["/geolink-personalizer"]
