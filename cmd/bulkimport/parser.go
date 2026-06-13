package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/barathsuresh/geolink/internal/models"
)

func parseLine(line string) (*models.GeoName, error) {
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil
	}
	fields := strings.Split(line, "\t")
	if len(fields) < 19 {
		return nil, fmt.Errorf("expected >= 19 fields, got %d", len(fields))
	}
	geonameID, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid geoname_id %q: %w", fields[0], err)
	}
	lat, err := strconv.ParseFloat(fields[4], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid latitude %q: %w", fields[4], err)
	}
	lon, err := strconv.ParseFloat(fields[5], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid longitude %q: %w", fields[5], err)
	}
	population, _ := strconv.ParseInt(fields[14], 10, 64)
	elevation := 0
	if fields[15] != "" {
		if e, err := strconv.Atoi(fields[15]); err == nil {
			elevation = e
		}
	}
	return &models.GeoName{
		GeonameID:      geonameID,
		Name:           fields[1],
		ASCIIName:      fields[2],
		AlternateNames: fields[3],
		Latitude:       lat,
		Longitude:      lon,
		FeatureClass:   fields[6],
		FeatureCode:    fields[7],
		CountryCode:    fields[8],
		Admin1Code:     fields[10],
		Admin2Code:     fields[11],
		Population:     population,
		Elevation:      elevation,
		Timezone:       fields[17],
		ModifiedAt:     fields[18],
	}, nil
}
