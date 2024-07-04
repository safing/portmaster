package geoip

import "strings"

const defaultCountryBasedAccuracy = 200

// AddCountryInfo adds missing country information to the location.
func (l *Location) AddCountryInfo() {
	// Check if we have the country code.
	if l.Country.Code == "" {
		return
	}

	// Check for anycast.
	if l.IsAnycast {
		// Reset data for anycast.
		l.Country.Code = "__"
		l.Coordinates.Latitude = 0
		l.Coordinates.Longitude = 0
	}

	// Get country info.
	info, ok := countries[l.Country.Code]
	if !ok {
		return
	}
	// Apply country info to location.
	l.Country = info

	// Use country center as location coordinates if unset.
	if l.Coordinates.Latitude == 0 && l.Coordinates.Longitude == 0 {
		l.Coordinates = info.Center
	}
}

// GetCountryInfo returns the country info of the given country code, or nil
// in case the data does not exist.
func GetCountryInfo(countryCode string) CountryInfo {
	info := countries[countryCode]
	return info
}

// CountryInfo holds additional information about countries.
type CountryInfo struct {
	Code      string `maxminddb:"iso_code"`
	Name      string
	Center    Coordinates
	Continent ContinentInfo
}

// ContinentInfo holds additional information about continents.
type ContinentInfo struct {
	Code   string
	Region string
	Name   string
}

// Add data to countries.
func init() {
	for code, country := range countries {
		// Set country code.
		country.Code = code

		// Derive continent code from continental region.
		country.Continent.Code, _, _ = strings.Cut(country.Continent.Region, "-")

		// Add continent name.
		switch country.Continent.Code {
		case "AF":
			country.Continent.Name = "Africa"
		case "AN":
			country.Continent.Name = "Antarctica"
		case "AS":
			country.Continent.Name = "Asia"
		case "EU":
			country.Continent.Name = "Europe"
		case "NA":
			country.Continent.Name = "North America"
		case "OC":
			country.Continent.Name = "Oceania"
		case "SA":
			country.Continent.Name = "South America"
		}

		// Add default accuracy radius.
		country.Center.AccuracyRadius = defaultCountryBasedAccuracy

		// Apply back to map.
		countries[code] = country
	}
}

var countries = map[string]CountryInfo{
	"__": {
		Name:   "Anycast",
		Center: Coordinates{AccuracyRadius: earthCircumferenceInKm},
	},
	"MN": {
		Name:      "Mongolia",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 46.000000, Longitude: 103.000000},
	},
	"BN": {
		Name:      "Brunei Darussalam",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 4.000000, Longitude: 114.000000},
	},
	"GI": {
		Name:      "Gibraltar",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 36.000000, Longitude: -5.000000},
	},
	"SO": {
		Name:      "Somalia",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: 5.000000, Longitude: 46.000000},
	},
	"GG": {
		Name:      "Guernsey",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 49.000000, Longitude: -2.000000},
	},
	"CL": {
		Name:      "Chile",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -35.000000, Longitude: -71.000000},
	},
	"LR": {
		Name:      "Liberia",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 6.000000, Longitude: -9.000000},
	},
	"TZ": {
		Name:      "Tanzania",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -6.000000, Longitude: 34.000000},
	},
	"MU": {
		Name:      "Mauritius",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -20.000000, Longitude: 57.000000},
	},
	"HM": {
		Name:      "Heard Island and McDonald Islands",
		Continent: ContinentInfo{Region: "OC-S"},
		Center:    Coordinates{Latitude: -53.000000, Longitude: 73.000000},
	},
	"AR": {
		Name:      "Argentina",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -38.000000, Longitude: -63.000000},
	},
	"BV": {
		Name:      "Bouvet Island",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -54.000000, Longitude: 3.000000},
	},
	"MS": {
		Name:      "Montserrat",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 16.000000, Longitude: -62.000000},
	},
	"PT": {
		Name:      "Portugal",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 39.000000, Longitude: -8.000000},
	},
	"BO": {
		Name:      "Bolivia",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -16.000000, Longitude: -63.000000},
	},
	"VC": {
		Name:      "Saint Vincent and the Grenadines",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: -61.000000},
	},
	"RO": {
		Name:      "Romania",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 45.000000, Longitude: 24.000000},
	},
	"MK": {
		Name:      "North Macedonia",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 41.000000, Longitude: 21.000000},
	},
	"UG": {
		Name:      "Uganda",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: 1.000000, Longitude: 32.000000},
	},
	"HN": {
		Name:      "Honduras",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 15.000000, Longitude: -86.000000},
	},
	"IS": {
		Name:      "Iceland",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 64.000000, Longitude: -19.000000},
	},
	"HR": {
		Name:      "Croatia",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 45.000000, Longitude: 15.000000},
	},
	"PL": {
		Name:      "Poland",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 51.000000, Longitude: 19.000000},
	},
	"TC": {
		Name:      "Turks and Caicos Islands",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 21.000000, Longitude: -71.000000},
	},
	"LC": {
		Name:      "Saint Lucia",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 13.000000, Longitude: -60.000000},
	},
	"JP": {
		Name:      "Japan",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 36.000000, Longitude: 138.000000},
	},
	"TN": {
		Name:      "Tunisia",
		Continent: ContinentInfo{Region: "AF-N"},
		Center:    Coordinates{Latitude: 33.000000, Longitude: 9.000000},
	},
	"GS": {
		Name:      "South Georgia and the South Sandwich Islands",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -54.000000, Longitude: -36.000000},
	},
	"MY": {
		Name:      "Malaysia",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 4.000000, Longitude: 101.000000},
	},
	"TT": {
		Name:      "Trinidad and Tobago",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 10.000000, Longitude: -61.000000},
	},
	"BE": {
		Name:      "Belgium",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 50.000000, Longitude: 4.000000},
	},
	"GU": {
		Name:      "Guam",
		Continent: ContinentInfo{Region: "OC-N"},
		Center:    Coordinates{Latitude: 13.000000, Longitude: 144.000000},
	},
	"NL": {
		Name:      "Netherlands",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 52.000000, Longitude: 5.000000},
	},
	"AF": {
		Name:      "Afghanistan",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 33.000000, Longitude: 67.000000},
	},
	"CK": {
		Name:      "Cook Islands",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -21.000000, Longitude: -159.000000},
	},
	"PM": {
		Name:      "Saint Pierre and Miquelon",
		Continent: ContinentInfo{Region: "NA-N"},
		Center:    Coordinates{Latitude: 46.000000, Longitude: -56.000000},
	},
	"OM": {
		Name:      "Oman",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 21.000000, Longitude: 55.000000},
	},
	"NP": {
		Name:      "Nepal",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 28.000000, Longitude: 84.000000},
	},
	"RS": {
		Name:      "Serbia",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 44.000000, Longitude: 21.000000},
	},
	"MW": {
		Name:      "Malawi",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -13.000000, Longitude: 34.000000},
	},
	"NE": {
		Name:      "Niger",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 17.000000, Longitude: 8.000000},
	},
	"BY": {
		Name:      "Belarus",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 53.000000, Longitude: 27.000000},
	},
	"TH": {
		Name:      "Thailand",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 15.000000, Longitude: 100.000000},
	},
	"CW": {
		Name:      "Curaçao",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: -68.000000},
	},
	"AS": {
		Name:      "American Samoa",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -14.000000, Longitude: -170.000000},
	},
	"BF": {
		Name:      "Burkina Faso",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: -1.000000},
	},
	"BR": {
		Name:      "Brazil",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -14.000000, Longitude: -51.000000},
	},
	"CX": {
		Name:      "Christmas Island",
		Continent: ContinentInfo{Region: "OC-S"},
		Center:    Coordinates{Latitude: -10.000000, Longitude: 105.000000},
	},
	"MG": {
		Name:      "Madagascar",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -18.000000, Longitude: 46.000000},
	},
	"CY": {
		Name:      "Cyprus",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 35.000000, Longitude: 33.000000},
	},
	"KW": {
		Name:      "Kuwait",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 29.000000, Longitude: 47.000000},
	},
	"IT": {
		Name:      "Italy",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 41.000000, Longitude: 12.000000},
	},
	"SJ": {
		Name:      "Svalbard and Jan Mayen",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 77.000000, Longitude: 23.000000},
	},
	"ZM": {
		Name:      "Zambia",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -13.000000, Longitude: 27.000000},
	},
	"TO": {
		Name:      "Tonga",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -21.000000, Longitude: -175.000000},
	},
	"EE": {
		Name:      "Estonia",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 58.000000, Longitude: 25.000000},
	},
	"LI": {
		Name:      "Liechtenstein",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 47.000000, Longitude: 9.000000},
	},
	"LB": {
		Name:      "Lebanon",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 33.000000, Longitude: 35.000000},
	},
	"DK": {
		Name:      "Denmark",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 56.000000, Longitude: 9.000000},
	},
	"LS": {
		Name:      "Lesotho",
		Continent: ContinentInfo{Region: "AF-S"},
		Center:    Coordinates{Latitude: -29.000000, Longitude: 28.000000},
	},
	"CM": {
		Name:      "Cameroon",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: 7.000000, Longitude: 12.000000},
	},
	"BH": {
		Name:      "Bahrain",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 25.000000, Longitude: 50.000000},
	},
	"NA": {
		Name:      "Namibia",
		Continent: ContinentInfo{Region: "AF-S"},
		Center:    Coordinates{Latitude: -22.000000, Longitude: 18.000000},
	},
	"ZA": {
		Name:      "South Africa",
		Continent: ContinentInfo{Region: "AF-S"},
		Center:    Coordinates{Latitude: -30.000000, Longitude: 22.000000},
	},
	"PH": {
		Name:      "Philippines",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: 121.000000},
	},
	"JM": {
		Name:      "Jamaica",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -77.000000},
	},
	"PS": {
		Name:      "Palestine",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 31.000000, Longitude: 35.000000},
	},
	"TM": {
		Name:      "Turkmenistan",
		Continent: ContinentInfo{Region: "AS-C"},
		Center:    Coordinates{Latitude: 38.000000, Longitude: 59.000000},
	},
	"SD": {
		Name:      "Sudan",
		Continent: ContinentInfo{Region: "AF-N"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: 30.000000},
	},
	"KN": {
		Name:      "Saint Kitts and Nevis",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 17.000000, Longitude: -62.000000},
	},
	"GF": {
		Name:      "French Guiana",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: 3.000000, Longitude: -53.000000},
	},
	"WS": {
		Name:      "Samoa",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -13.000000, Longitude: -172.000000},
	},
	"KE": {
		Name:      "Kenya",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: 0.000000, Longitude: 37.000000},
	},
	"CG": {
		Name:      "Congo",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: 0.000000, Longitude: 15.000000},
	},
	"FJ": {
		Name:      "Fiji",
		Continent: ContinentInfo{Region: "OC-C"},
		Center:    Coordinates{Latitude: -16.000000, Longitude: 179.000000},
	},
	"BL": {
		Name:      "Saint Barthélemy",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 17.000000, Longitude: -62.000000},
	},
	"TD": {
		Name:      "Chad",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: 15.000000, Longitude: 18.000000},
	},
	"TW": {
		Name:      "Taiwan",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 23.000000, Longitude: 120.000000},
	},
	"SA": {
		Name:      "Saudi Arabia",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 23.000000, Longitude: 45.000000},
	},
	"CO": {
		Name:      "Colombia",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: 4.000000, Longitude: -74.000000},
	},
	"FR": {
		Name:      "France",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 46.000000, Longitude: 2.000000},
	},
	"WF": {
		Name:      "Wallis and Futuna",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -13.000000, Longitude: -177.000000},
	},
	"QA": {
		Name:      "Qatar",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 25.000000, Longitude: 51.000000},
	},
	"IO": {
		Name:      "British Indian Ocean Territory",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -6.000000, Longitude: 71.000000},
	},
	"LT": {
		Name:      "Lithuania",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 55.000000, Longitude: 23.000000},
	},
	"IE": {
		Name:      "Ireland",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 53.000000, Longitude: -8.000000},
	},
	"GW": {
		Name:      "Guinea-Bissau",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 11.000000, Longitude: -15.000000},
	},
	"PE": {
		Name:      "Peru",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -9.000000, Longitude: -75.000000},
	},
	"MA": {
		Name:      "Morocco",
		Continent: ContinentInfo{Region: "AF-N"},
		Center:    Coordinates{Latitude: 31.000000, Longitude: -7.000000},
	},
	"CR": {
		Name:      "Costa Rica",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 9.000000, Longitude: -83.000000},
	},
	"FK": {
		Name:      "Falkland Islands (Malvinas)",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -51.000000, Longitude: -59.000000},
	},
	"PW": {
		Name:      "Palau",
		Continent: ContinentInfo{Region: "OC-N"},
		Center:    Coordinates{Latitude: 7.000000, Longitude: 134.000000},
	},
	"NC": {
		Name:      "New Caledonia",
		Continent: ContinentInfo{Region: "OC-C"},
		Center:    Coordinates{Latitude: -20.000000, Longitude: 165.000000},
	},
	"AM": {
		Name:      "Armenia",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 40.000000, Longitude: 45.000000},
	},
	"CU": {
		Name:      "Cuba",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 21.000000, Longitude: -77.000000},
	},
	"DE": {
		Name:      "Germany",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 51.000000, Longitude: 10.000000},
	},
	"MT": {
		Name:      "Malta",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 35.000000, Longitude: 14.000000},
	},
	"YE": {
		Name:      "Yemen",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 15.000000, Longitude: 48.000000},
	},
	"BA": {
		Name:      "Bosnia and Herzegovina",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 43.000000, Longitude: 17.000000},
	},
	"MP": {
		Name:      "Northern Mariana Islands",
		Continent: ContinentInfo{Region: "OC-N"},
		Center:    Coordinates{Latitude: 17.000000, Longitude: 145.000000},
	},
	"PY": {
		Name:      "Paraguay",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -23.000000, Longitude: -58.000000},
	},
	"MO": {
		Name:      "Macao",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 22.000000, Longitude: 113.000000},
	},
	"SH": {
		Name:      "Saint Helena",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: -24.000000, Longitude: -10.000000},
	},
	"PN": {
		Name:      "Pitcairn",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -24.000000, Longitude: -127.000000},
	},
	"GM": {
		Name:      "Gambia",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 13.000000, Longitude: -15.000000},
	},
	"TG": {
		Name:      "Togo",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 8.000000, Longitude: 0.000000},
	},
	"AT": {
		Name:      "Austria",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 47.000000, Longitude: 14.000000},
	},
	"GT": {
		Name:      "Guatemala",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 15.000000, Longitude: -90.000000},
	},
	"AE": {
		Name:      "United Arab Emirates",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 23.000000, Longitude: 53.000000},
	},
	"KR": {
		Name:      "South Korea",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 35.000000, Longitude: 127.000000},
	},
	"JE": {
		Name:      "Jersey",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 49.000000, Longitude: -2.000000},
	},
	"LV": {
		Name:      "Latvia",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 56.000000, Longitude: 24.000000},
	},
	"AW": {
		Name:      "Aruba",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: -69.000000},
	},
	"AO": {
		Name:      "Angola",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: -11.000000, Longitude: 17.000000},
	},
	"VE": {
		Name:      "Venezuela",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: 6.000000, Longitude: -66.000000},
	},
	"AG": {
		Name:      "Antigua and Barbuda",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 17.000000, Longitude: -61.000000},
	},
	"NU": {
		Name:      "Niue",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -19.000000, Longitude: -169.000000},
	},
	"KY": {
		Name:      "Cayman Islands",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 19.000000, Longitude: -80.000000},
	},
	"IM": {
		Name:      "Isle of Man",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 54.000000, Longitude: -4.000000},
	},
	"FM": {
		Name:      "Micronesia",
		Continent: ContinentInfo{Region: "OC-N"},
		Center:    Coordinates{Latitude: 7.000000, Longitude: 150.000000},
	},
	"SB": {
		Name:      "Solomon Islands",
		Continent: ContinentInfo{Region: "OC-C"},
		Center:    Coordinates{Latitude: -9.000000, Longitude: 160.000000},
	},
	"LU": {
		Name:      "Luxembourg",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 49.000000, Longitude: 6.000000},
	},
	"MF": {
		Name:      "Saint Martin",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -63.000000},
	},
	"AQ": {
		Name:      "Antarctica",
		Continent: ContinentInfo{Region: "AN"},
		Center:    Coordinates{Latitude: -75.000000, Longitude: 0.000000},
	},
	"SC": {
		Name:      "Seychelles",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -4.000000, Longitude: 55.000000},
	},
	"TL": {
		Name:      "Timor-Leste",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: -8.000000, Longitude: 125.000000},
	},
	"CC": {
		Name:      "Cocos (Keeling) Islands",
		Continent: ContinentInfo{Region: "OC-S"},
		Center:    Coordinates{Latitude: -12.000000, Longitude: 96.000000},
	},
	"ST": {
		Name:      "Sao Tome and Principe",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: 0.000000, Longitude: 6.000000},
	},
	"NO": {
		Name:      "Norway",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 60.000000, Longitude: 8.000000},
	},
	"CF": {
		Name:      "Central African Republic",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: 6.000000, Longitude: 20.000000},
	},
	"MR": {
		Name:      "Mauritania",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 21.000000, Longitude: -10.000000},
	},
	"NI": {
		Name:      "Nicaragua",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: -85.000000},
	},
	"AI": {
		Name:      "Anguilla",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -63.000000},
	},
	"AZ": {
		Name:      "Azerbaijan",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 40.000000, Longitude: 47.000000},
	},
	"US": {
		Name:      "United States of America",
		Continent: ContinentInfo{Region: "NA-N"},
		Center:    Coordinates{Latitude: 37.000000, Longitude: -95.000000},
	},
	"LA": {
		Name:      "Lao",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 19.000000, Longitude: 102.000000},
	},
	"BB": {
		Name:      "Barbados",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 13.000000, Longitude: -59.000000},
	},
	"CD": {
		Name:      "DR Congo",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: -4.000000, Longitude: 21.000000},
	},
	"TK": {
		Name:      "Tokelau",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -8.000000, Longitude: -171.000000},
	},
	"KZ": {
		Name:      "Kazakhstan",
		Continent: ContinentInfo{Region: "AS-C"},
		Center:    Coordinates{Latitude: 48.000000, Longitude: 66.000000},
	},
	"DM": {
		Name:      "Dominica",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 15.000000, Longitude: -61.000000},
	},
	"EG": {
		Name:      "Egypt",
		Continent: ContinentInfo{Region: "AF-N"},
		Center:    Coordinates{Latitude: 26.000000, Longitude: 30.000000},
	},
	"GH": {
		Name:      "Ghana",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 7.000000, Longitude: -1.000000},
	},
	"BI": {
		Name:      "Burundi",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -3.000000, Longitude: 29.000000},
	},
	"NZ": {
		Name:      "New Zealand",
		Continent: ContinentInfo{Region: "OC-S"},
		Center:    Coordinates{Latitude: -40.000000, Longitude: 174.000000},
	},
	"BJ": {
		Name:      "Benin",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 9.000000, Longitude: 2.000000},
	},
	"HU": {
		Name:      "Hungary",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 47.000000, Longitude: 19.000000},
	},
	"BD": {
		Name:      "Bangladesh",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 23.000000, Longitude: 90.000000},
	},
	"NF": {
		Name:      "Norfolk Island",
		Continent: ContinentInfo{Region: "OC-S"},
		Center:    Coordinates{Latitude: -29.000000, Longitude: 167.000000},
	},
	"LY": {
		Name:      "Libya",
		Continent: ContinentInfo{Region: "AF-N"},
		Center:    Coordinates{Latitude: 26.000000, Longitude: 17.000000},
	},
	"TV": {
		Name:      "Tuvalu",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -7.000000, Longitude: 177.000000},
	},
	"ZW": {
		Name:      "Zimbabwe",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -19.000000, Longitude: 29.000000},
	},
	"NG": {
		Name:      "Nigeria",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 9.000000, Longitude: 8.000000},
	},
	"GD": {
		Name:      "Grenada",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: -61.000000},
	},
	"SM": {
		Name:      "San Marino",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 43.000000, Longitude: 12.000000},
	},
	"RU": {
		Name:      "Russian Federation",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 61.000000, Longitude: 105.000000},
	},
	"DZ": {
		Name:      "Algeria",
		Continent: ContinentInfo{Region: "AF-N"},
		Center:    Coordinates{Latitude: 28.000000, Longitude: 1.000000},
	},
	"DO": {
		Name:      "Dominican Republic",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -70.000000},
	},
	"SI": {
		Name:      "Slovenia",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 46.000000, Longitude: 14.000000},
	},
	"BZ": {
		Name:      "Belize",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 17.000000, Longitude: -88.000000},
	},
	"DJ": {
		Name:      "Djibouti",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: 11.000000, Longitude: 42.000000},
	},
	"GN": {
		Name:      "Guinea",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 9.000000, Longitude: -9.000000},
	},
	"VN": {
		Name:      "Viet Nam",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 14.000000, Longitude: 108.000000},
	},
	"IR": {
		Name:      "Iran",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 32.000000, Longitude: 53.000000},
	},
	"KG": {
		Name:      "Kyrgyzstan",
		Continent: ContinentInfo{Region: "AS-C"},
		Center:    Coordinates{Latitude: 41.000000, Longitude: 74.000000},
	},
	"ML": {
		Name:      "Mali",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 17.000000, Longitude: -3.000000},
	},
	"GP": {
		Name:      "Guadeloupe",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 16.000000, Longitude: -62.000000},
	},
	"FI": {
		Name:      "Finland",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 61.000000, Longitude: 25.000000},
	},
	"UA": {
		Name:      "Ukraine",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 48.000000, Longitude: 31.000000},
	},
	"KP": {
		Name:      "North Korea (DPRK)",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 40.000000, Longitude: 127.000000},
	},
	"BT": {
		Name:      "Bhutan",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 27.000000, Longitude: 90.000000},
	},
	"BG": {
		Name:      "Bulgaria",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 42.000000, Longitude: 25.000000},
	},
	"MM": {
		Name:      "Myanmar",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 21.000000, Longitude: 95.000000},
	},
	"PK": {
		Name:      "Pakistan",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 30.000000, Longitude: 69.000000},
	},
	"KI": {
		Name:      "Kiribati",
		Continent: ContinentInfo{Region: "OC-N"},
		Center:    Coordinates{Latitude: -3.000000, Longitude: -168.000000},
	},
	"GL": {
		Name:      "Greenland",
		Continent: ContinentInfo{Region: "NA-N"},
		Center:    Coordinates{Latitude: 71.000000, Longitude: -42.000000},
	},
	"PG": {
		Name:      "Papua New Guinea",
		Continent: ContinentInfo{Region: "OC-C"},
		Center:    Coordinates{Latitude: -6.000000, Longitude: 143.000000},
	},
	"PF": {
		Name:      "French Polynesia",
		Continent: ContinentInfo{Region: "OC-E"},
		Center:    Coordinates{Latitude: -17.000000, Longitude: -149.000000},
	},
	"VU": {
		Name:      "Vanuatu",
		Continent: ContinentInfo{Region: "OC-C"},
		Center:    Coordinates{Latitude: -15.000000, Longitude: 166.000000},
	},
	"HT": {
		Name:      "Haiti",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -72.000000},
	},
	"SV": {
		Name:      "El Salvador",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 13.000000, Longitude: -88.000000},
	},
	"EC": {
		Name:      "Ecuador",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -1.000000, Longitude: -78.000000},
	},
	"KM": {
		Name:      "Comoros",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -11.000000, Longitude: 43.000000},
	},
	"VI": {
		Name:      "Virgin Islands (U.S.)",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -64.000000},
	},
	"YT": {
		Name:      "Mayotte",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -12.000000, Longitude: 45.000000},
	},
	"ET": {
		Name:      "Ethiopia",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: 9.000000, Longitude: 40.000000},
	},
	"JO": {
		Name:      "Jordan",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 30.000000, Longitude: 36.000000},
	},
	"RE": {
		Name:      "Réunion",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -21.000000, Longitude: 55.000000},
	},
	"NR": {
		Name:      "Nauru",
		Continent: ContinentInfo{Region: "OC-N"},
		Center:    Coordinates{Latitude: 0.000000, Longitude: 166.000000},
	},
	"HK": {
		Name:      "Hong Kong",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 22.000000, Longitude: 114.000000},
	},
	"AU": {
		Name:      "Australia",
		Continent: ContinentInfo{Region: "OC-S"},
		Center:    Coordinates{Latitude: -25.000000, Longitude: 133.000000},
	},
	"FO": {
		Name:      "Faroe Islands",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 61.000000, Longitude: -6.000000},
	},
	"IQ": {
		Name:      "Iraq",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 33.000000, Longitude: 43.000000},
	},
	"GE": {
		Name:      "Georgia",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 42.000000, Longitude: 43.000000},
	},
	"UZ": {
		Name:      "Uzbekistan",
		Continent: ContinentInfo{Region: "AS-C"},
		Center:    Coordinates{Latitude: 41.000000, Longitude: 64.000000},
	},
	"IN": {
		Name:      "India",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 20.000000, Longitude: 78.000000},
	},
	"MX": {
		Name:      "Mexico",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 23.000000, Longitude: -102.000000},
	},
	"ER": {
		Name:      "Eritrea",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: 15.000000, Longitude: 39.000000},
	},
	"AL": {
		Name:      "Albania",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 41.000000, Longitude: 20.000000},
	},
	"GY": {
		Name:      "Guyana",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: 4.000000, Longitude: -58.000000},
	},
	"CA": {
		Name:      "Canada",
		Continent: ContinentInfo{Region: "NA-N"},
		Center:    Coordinates{Latitude: 56.000000, Longitude: -106.000000},
	},
	"SY": {
		Name:      "Syrian Arab Republic",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 34.000000, Longitude: 38.000000},
	},
	"SG": {
		Name:      "Singapore",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 1.000000, Longitude: 103.000000},
	},
	"VG": {
		Name:      "Virgin Islands (British)",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -64.000000},
	},
	"MC": {
		Name:      "Monaco",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 43.000000, Longitude: 7.000000},
	},
	"BM": {
		Name:      "Bermuda",
		Continent: ContinentInfo{Region: "NA-N"},
		Center:    Coordinates{Latitude: 32.000000, Longitude: -64.000000},
	},
	"SX": {
		Name:      "Sint Maarten",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -63.000000},
	},
	"SR": {
		Name:      "Suriname",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: 3.000000, Longitude: -56.000000},
	},
	"MD": {
		Name:      "Moldova",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 47.000000, Longitude: 28.000000},
	},
	"CZ": {
		Name:      "Czechia",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 49.000000, Longitude: 15.000000},
	},
	"GQ": {
		Name:      "Equatorial Guinea",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: 1.000000, Longitude: 10.000000},
	},
	"TF": {
		Name:      "French Southern Territories",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -49.000000, Longitude: 69.000000},
	},
	"CI": {
		Name:      "Côte d'Ivoire",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 7.000000, Longitude: -5.000000},
	},
	"VA": {
		Name:      "Holy See",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 41.000000, Longitude: 12.000000},
	},
	"SN": {
		Name:      "Senegal",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 14.000000, Longitude: -14.000000},
	},
	"PR": {
		Name:      "Puerto Rico",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 18.000000, Longitude: -66.000000},
	},
	"ID": {
		Name:      "Indonesia",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 0.000000, Longitude: 113.000000},
	},
	"BS": {
		Name:      "Bahamas",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 25.000000, Longitude: -77.000000},
	},
	"CV": {
		Name:      "Cabo Verde",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 16.000000, Longitude: -24.000000},
	},
	"AD": {
		Name:      "Andorra",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 42.000000, Longitude: 1.000000},
	},
	"SK": {
		Name:      "Slovakia",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 48.000000, Longitude: 19.000000},
	},
	"MV": {
		Name:      "Maldives",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 3.000000, Longitude: 73.000000},
	},
	"ME": {
		Name:      "Montenegro",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 42.000000, Longitude: 19.000000},
	},
	"LK": {
		Name:      "Sri Lanka",
		Continent: ContinentInfo{Region: "AS-S"},
		Center:    Coordinates{Latitude: 7.000000, Longitude: 80.000000},
	},
	"KH": {
		Name:      "Cambodia",
		Continent: ContinentInfo{Region: "AS-SE"},
		Center:    Coordinates{Latitude: 12.000000, Longitude: 104.000000},
	},
	"GR": {
		Name:      "Greece",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 39.000000, Longitude: 21.000000},
	},
	"SL": {
		Name:      "Sierra Leone",
		Continent: ContinentInfo{Region: "AF-W"},
		Center:    Coordinates{Latitude: 8.000000, Longitude: -11.000000},
	},
	"XK": {
		Name:      "Kosovo",
		Continent: ContinentInfo{Region: "EU-E"},
		Center:    Coordinates{Latitude: 42.000000, Longitude: 20.000000},
	},
	"TJ": {
		Name:      "Tajikistan",
		Continent: ContinentInfo{Region: "AS-C"},
		Center:    Coordinates{Latitude: 38.000000, Longitude: 71.000000},
	},
	"SE": {
		Name:      "Sweden",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 60.000000, Longitude: 18.000000},
	},
	"GA": {
		Name:      "Gabon",
		Continent: ContinentInfo{Region: "AF-C"},
		Center:    Coordinates{Latitude: 0.000000, Longitude: 11.000000},
	},
	"UY": {
		Name:      "Uruguay",
		Continent: ContinentInfo{Region: "SA"},
		Center:    Coordinates{Latitude: -32.000000, Longitude: -55.000000},
	},
	"MZ": {
		Name:      "Mozambique",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -18.000000, Longitude: 35.000000},
	},
	"PA": {
		Name:      "Panama",
		Continent: ContinentInfo{Region: "NA-S"},
		Center:    Coordinates{Latitude: 8.000000, Longitude: -80.000000},
	},
	"SZ": {
		Name:      "Eswatini",
		Continent: ContinentInfo{Region: "AF-S"},
		Center:    Coordinates{Latitude: -26.000000, Longitude: 31.000000},
	},
	"IL": {
		Name:      "Israel",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 31.000000, Longitude: 34.000000},
	},
	"GB": {
		Name:      "United Kingdom",
		Continent: ContinentInfo{Region: "EU-N"},
		Center:    Coordinates{Latitude: 55.000000, Longitude: -3.000000},
	},
	"ES": {
		Name:      "Spain",
		Continent: ContinentInfo{Region: "EU-S"},
		Center:    Coordinates{Latitude: 40.000000, Longitude: -3.000000},
	},
	"RW": {
		Name:      "Rwanda",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: -1.000000, Longitude: 29.000000},
	},
	"EH": {
		Name:      "Western Sahara",
		Continent: ContinentInfo{Region: "AF-N"},
		Center:    Coordinates{Latitude: 24.000000, Longitude: -12.000000},
	},
	"MH": {
		Name:      "Marshall Islands",
		Continent: ContinentInfo{Region: "OC-N"},
		Center:    Coordinates{Latitude: 7.000000, Longitude: 171.000000},
	},
	"MQ": {
		Name:      "Martinique",
		Continent: ContinentInfo{Region: "NA-E"},
		Center:    Coordinates{Latitude: 14.000000, Longitude: -61.000000},
	},
	"CH": {
		Name:      "Switzerland",
		Continent: ContinentInfo{Region: "EU-W"},
		Center:    Coordinates{Latitude: 46.000000, Longitude: 8.000000},
	},
	"CN": {
		Name:      "China",
		Continent: ContinentInfo{Region: "AS-E"},
		Center:    Coordinates{Latitude: 35.000000, Longitude: 104.000000},
	},
	"TR": {
		Name:      "Turkey",
		Continent: ContinentInfo{Region: "AS-W"},
		Center:    Coordinates{Latitude: 38.000000, Longitude: 35.000000},
	},
	"BW": {
		Name:      "Botswana",
		Continent: ContinentInfo{Region: "AF-S"},
		Center:    Coordinates{Latitude: -22.000000, Longitude: 24.000000},
	},
	"SS": {
		Name:      "South Sudan",
		Continent: ContinentInfo{Region: "AF-E"},
		Center:    Coordinates{Latitude: 4.000000, Longitude: 31.000000},
	},
}
