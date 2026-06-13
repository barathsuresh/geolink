// internal/models/geoname.go
// GeoName domain model and feature-code metadata.
package models

// GeoName represents a single record from the GeoNames dataset.
// Column order matches the 19-column allCountries.txt TSV format.
// See: https://download.geonames.org/export/dump/readme.txt
type GeoName struct {
	GeonameID      int64   `json:"geoname_id"`
	Name           string  `json:"name"`
	ASCIIName      string  `json:"ascii_name"`
	AlternateNames string  `json:"alternate_names"` // pipe-separated
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	FeatureClass   string  `json:"feature_class"` // single char: A,H,L,P,R,S,T,U,V
	FeatureCode    string  `json:"feature_code"`
	CountryCode    string  `json:"country_code"`
	Admin1Code     string  `json:"admin1_code"`
	Admin2Code     string  `json:"admin2_code"`
	Population     int64   `json:"population"`
	Elevation      int     `json:"elevation"`
	Timezone       string  `json:"timezone"`
	ModifiedAt     string  `json:"modified_at"` // ISO date string, e.g. "2024-03-15"
}

// FeatureCodeLabels maps GeoNames feature codes to human-readable labels.
var FeatureCodeLabels = map[string]string{
	// ── Populated Places (P) ──────────────────────────────────────────────────
	"PPLC":  "Capital City",
	"PPLA":  "State Capital",
	"PPLA2": "District Capital",
	"PPLA3": "Sub-district Capital",
	"PPLA4": "Local Capital",
	"PPLA5": "Local Capital",
	"PPL":   "City / Town",
	"PPLF":  "Farm Village",
	"PPLG":  "Seat of Government",
	"PPLH":  "Historical Settlement",
	"PPLL":  "Local Village",
	"PPLQ":  "Abandoned Settlement",
	"PPLR":  "Religious Settlement",
	"PPLS":  "Populated Places",
	"PPLW":  "Destroyed Settlement",
	"PPLX":  "Neighbourhood",
	"STLMT": "Israeli Settlement",

	// ── Administrative Divisions (A) ──────────────────────────────────────────
	"PCLI":  "Country",
	"PCLD":  "Dependent Territory",
	"PCLF":  "Semi-independent Territory",
	"PCLIX": "Section of Country",
	"PCLH":  "Historical Country",
	"PCLS":  "Self-governing Territory",
	"ADM1":  "State / Province",
	"ADM1H": "Historical State",
	"ADM2":  "County / District",
	"ADM2H": "Historical County",
	"ADM3":  "Sub-district",
	"ADM3H": "Historical Sub-district",
	"ADM4":  "Municipality",
	"ADM4H": "Historical Municipality",
	"ADM5":  "Sub-municipality",
	"ADM5H": "Historical Sub-municipality",
	"ADMD":  "Administrative District",
	"ADMDH": "Historical Admin District",
	"LTER":  "Leased Territory",
	"PRSH":  "Parish",
	"TERR":  "Territory",
	"ZN":    "Zone",
	"ZNB":   "Buffer Zone",
	"RGN":   "Region",
	"RGNE":  "Economic Region",
	"RGNL":  "Lake Region",
	"RGNH":  "Historical Region",

	// ── Spots / Buildings / Farms (S) ─────────────────────────────────────────
	"AIRP":  "Airport",
	"AIRB":  "Air Base",
	"AIRF":  "Airfield",
	"AIRT":  "Heliport",
	"AIRQ":  "Abandoned Airport",
	"RSTN":  "Railway Station",
	"RSTNQ": "Abandoned Railway Station",
	"BUSTN": "Bus Station",
	"BUSTP": "Bus Stop",
	"PORT":  "Port",
	"PRTQ":  "Abandoned Port",
	"MSQE":  "Mosque",
	"CHRCH": "Church",
	"TMPL":  "Temple",
	"CVNT":  "Convent",
	"MNST":  "Monastery",
	"CSTL":  "Castle",
	"HSTS":  "Historical Site",
	"RUIN":  "Ruins",
	"MUS":   "Museum",
	"LIBR":  "Library",
	"THTR":  "Theatre",
	"STDM":  "Stadium",
	"MALL":  "Mall",
	"HTL":   "Hotel",
	"HSP":   "Hospital",
	"SCH":   "School",
	"UNIV":  "University",
	"COLL":  "College",
	"MILB":  "Military Base",
	"INSM":  "Military Installation",
	"OBPT":  "Observation Point",
	"LTHSE": "Lighthouse",
	"BLDG":  "Building",
	"EST":   "Estate",
	"FRM":   "Farm",
	"FRMS":  "Farms",
	"CAVE":  "Cave",
	"ZOO":   "Zoo",
	"RECG":  "Golf Course",
	"RECR":  "Race Circuit",
	"RECRES": "Resort",

	// ── Hydrographic (H) ──────────────────────────────────────────────────────
	"STM":  "River / Stream",
	"STMI": "Intermittent Stream",
	"LK":   "Lake",
	"LKI":  "Intermittent Lake",
	"LKS":  "Lakes",
	"BAY":  "Bay",
	"BAYS": "Bays",
	"COVE": "Cove",
	"SEA":  "Sea",
	"OCN":  "Ocean",
	"GULF": "Gulf",
	"STRM": "Strait",
	"CHAN": "Channel",
	"FALL": "Waterfall",
	"RSVR": "Reservoir",
	"SPNG": "Spring",
	"SWMP": "Swamp",
	"ESTY": "Estuary",
	"BCH":  "Beach",
	"BCHS": "Beaches",

	// ── Mountains / Hills / Terrain (T) ───────────────────────────────────────
	"MT":    "Mountain",
	"MTS":   "Mountain Range",
	"PK":    "Peak",
	"PKS":   "Peaks",
	"HILL":  "Hill",
	"HILLS": "Hills",
	"CLF":   "Cliff",
	"CAPE":  "Cape",
	"PEN":   "Peninsula",
	"ISTH":  "Isthmus",
	"PLN":   "Plain",
	"PLNS":  "Plains",
	"VAL":   "Valley",
	"VALS":  "Valleys",
	"DSRT":  "Desert",
	"GRGE":  "Gorge",
	"PASS":  "Mountain Pass",
	"PLAT":  "Plateau",
	"MESA":  "Mesa",
	"RDGE":  "Ridge",
	"SAND":  "Sand Area",
	"ATOL":  "Atoll",

	// ── Islands (T subset) ────────────────────────────────────────────────────
	"ISL":  "Island",
	"ISLS": "Islands",
	"ISLET": "Islet",
	"ISLF": "Artificial Island",

	// ── Vegetation / Land (L/V) ───────────────────────────────────────────────
	"PRK":  "Park",
	"RESV": "Nature Reserve",
	"FRST": "Forest",
	"MOOR": "Moor",
	"OILF": "Oil Field",
	"GRAZ": "Grazing Area",

	// ── Roads / Railroads (R) ─────────────────────────────────────────────────
	"RD":   "Road",
	"RDU":  "Underpass",
	"RDCUT": "Road Cut",
	"TRL":  "Trail",
	"RR":   "Railroad",
	"RNJCT": "Railroad Junction",
	"TNLRR": "Railroad Tunnel",
	"BDG":  "Bridge",
	"BDGQ": "Ruined Bridge",
}

// FeatureClassLabels provides a fallback label based on the single-char feature class.
var FeatureClassLabels = map[string]string{
	"P": "Place",
	"A": "Administrative",
	"S": "Spot / Building",
	"T": "Terrain",
	"H": "Hydrographic",
	"R": "Road / Railroad",
	"L": "Land Area",
	"V": "Vegetation",
	"U": "Undersea",
}

// GetFeatureLabel returns the human-readable label for a GeoNames feature code.
// Falls back to the feature class label, then the raw code if nothing matches.
func GetFeatureLabel(code string) string {
	if label, ok := FeatureCodeLabels[code]; ok {
		return label
	}
	// Use feature class (first char) as fallback.
	if len(code) > 0 {
		if classLabel, ok := FeatureClassLabels[string(code[0])]; ok {
			return classLabel
		}
	}
	return code
}
