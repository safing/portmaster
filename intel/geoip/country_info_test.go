package geoip

import (
	"testing"
)

func TestCountryInfo(t *testing.T) {
	t.Parallel()

	for key, country := range countries {
		if key != country.ID {
			t.Errorf("%s has a wrong ID of %q", key, country.ID)
		}
		if country.Name == "" {
			t.Errorf("%s is missing name", key)
		}
		if country.Region == "" {
			t.Errorf("%s is missing region", key)
		}
		if country.ContinentCode == "" {
			t.Errorf("%s is missing continent", key)
		}
		if country.Center.Latitude == 0 && country.Center.Longitude == 0 {
			t.Errorf("%s is missing coords", key)
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
