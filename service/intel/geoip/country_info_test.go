package geoip

import (
	"strings"
	"testing"
)

func TestCountryInfo(t *testing.T) {
	t.Parallel()

	for key, country := range countries {
		// Skip special anycast country.
		if key == "__" {
			continue
		}

		if key != country.Code {
			t.Errorf("%s has a wrong country code of %q", key, country.Code)
		}
		if country.Name == "" {
			t.Errorf("%s is missing name", key)
		}
		if country.Continent.Code == "" {
			t.Errorf("%s is missing continent", key)
		}
		if country.Continent.Region == "" {
			t.Errorf("%s is missing continent region", key)
		}
		if country.Continent.Name == "" {
			t.Errorf("%s is missing continent name", key)
		}
		generatedContinentCode, _, _ := strings.Cut(country.Continent.Region, "-")
		if country.Continent.Code != generatedContinentCode {
			t.Errorf("%s is has wrong continent code or region", key)
		}
		if country.Center.Latitude == 0 && country.Center.Longitude == 0 {
			t.Errorf("%s is missing coords", key)
		}
		if country.Center.AccuracyRadius == 0 {
			t.Errorf("%s is missing accuracy radius", key)
		}

		// Generate map source from data:
		// fmt.Printf(
		// 	`"%s": {Name:%q,Region:%q,ContinentCode:%q,Center:Coordinates{AccuracyRadius:%d,Latitude:%f,Longitude:%f},},`,
		// 	key,
		// 	country.Name,
		// 	country.Region,
		// 	country.ContinentCode,
		// 	country.Center.AccuracyRadius,
		// 	country.Center.Latitude,
		// 	country.Center.Longitude,
		// )
		// fmt.Println()
	}
	if len(countries) < 247 {
		t.Errorf("dataset only includes %d countries", len(countries))
	}
}
