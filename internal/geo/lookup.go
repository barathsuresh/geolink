// internal/geo/lookup.go
// Geolocates a public IP address using the free ip-api.com JSON API.
// Returns nil without error for loopback / private / unresolvable addresses.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Location holds the geographic result of an IP lookup.
type Location struct {
	IP          string
	CountryCode string  // ISO 3166-1 alpha-2, e.g. "US"
	Country     string  // full name, e.g. "United States"
	City        string
	Lat         float64
	Lng         float64
}

// ipAPIResponse mirrors the ip-api.com JSON fields we care about.
type ipAPIResponse struct {
	Status      string  `json:"status"` // "success" | "fail"
	CountryCode string  `json:"countryCode"`
	Country     string  `json:"country"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

var httpClient = &http.Client{Timeout: 3 * time.Second}

// LookupIP geolocates the given IP.
// Returns (nil, nil) for loopback / private addresses or any resolution failure.
func LookupIP(ctx context.Context, ip string) (*Location, error) {
	if isPrivate(ip) {
		return nil, nil
	}

	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,countryCode,city,lat,lon", ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("geo lookup request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil // network error — fail silently, caller will use fallback
	}
	defer resp.Body.Close()

	var result ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil
	}
	if result.Status != "success" {
		return nil, nil
	}

	return &Location{
		IP:          ip,
		CountryCode: result.CountryCode,
		Country:     result.Country,
		City:        result.City,
		Lat:         result.Lat,
		Lng:         result.Lon,
	}, nil
}

// isPrivate returns true for loopback and RFC-1918 / RFC-4193 private ranges.
func isPrivate(ipStr string) bool {
	if ipStr == "" || ipStr == "::1" || ipStr == "127.0.0.1" {
		return true
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true
	}
	private := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7",
	}
	for _, cidr := range private {
		_, block, _ := net.ParseCIDR(cidr)
		if block != nil && block.Contains(ip) {
			return true
		}
	}
	return false
}
