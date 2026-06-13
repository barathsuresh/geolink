FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /geolink-bulkimport ./cmd/bulkimport

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /geolink-bulkimport /app/geolink-bulkimport
COPY --from=builder /app/migrations /app/migrations
ENTRYPOINT ["/app/geolink-bulkimport"]
