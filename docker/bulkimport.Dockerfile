FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /geolink-bulkimport ./cmd/bulkimport

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /geolink-bulkimport /geolink-bulkimport
ENTRYPOINT ["/geolink-bulkimport"]
