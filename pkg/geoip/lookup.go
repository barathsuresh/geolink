// pkg/geoip/lookup.go
// IP → country → continent resolution utilities.
// Used by the search handler and personalizer to enrich search events.
package geoip

// CountryContinent maps ISO 3166-1 alpha-2 country codes to continent names.
var CountryContinent = map[string]string{
	// North America
	"US": "North America",
	"CA": "North America",
	"MX": "North America",

	// South America
	"BR": "South America",
	"AR": "South America",
	"CO": "South America",
	"CL": "South America",
	"PE": "South America",
	"VE": "South America",

	// Europe
	"FR": "Europe",
	"DE": "Europe",
	"GB": "Europe",
	"IT": "Europe",
	"ES": "Europe",
	"PL": "Europe",
	"NL": "Europe",
	"SE": "Europe",
	"NO": "Europe",
	"FI": "Europe",
	"PT": "Europe",
	"GR": "Europe",
	"CZ": "Europe",
	"RO": "Europe",
	"HU": "Europe",
	"RU": "Europe",

	// Africa
	"NG": "Africa",
	"GH": "Africa",
	"KE": "Africa",
	"ZA": "Africa",
	"EG": "Africa",
	"ET": "Africa",
	"TZ": "Africa",
	"UG": "Africa",
	"CM": "Africa",
	"CI": "Africa",

	// Asia
	"PH": "Asia",
	"IN": "Asia",
	"CN": "Asia",
	"JP": "Asia",
	"ID": "Asia",
	"KR": "Asia",
	"TH": "Asia",
	"VN": "Asia",
	"PK": "Asia",
	"BD": "Asia",
	"MM": "Asia",
	"MY": "Asia",
	"SG": "Asia",
	"TR": "Asia",

	// Oceania
	"AU": "Oceania",
	"NZ": "Oceania",
	"PG": "Oceania",
	"FJ": "Oceania",
}

// GetContinent returns the continent name for a given ISO 3166-1 alpha-2
// country code. Returns "Unknown" if the country code is not in the map.
func GetContinent(countryCode string) string {
	if c, ok := CountryContinent[countryCode]; ok {
		return c
	}
	return "Unknown"
}
