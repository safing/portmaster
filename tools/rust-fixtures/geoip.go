// GeoIP data extraction for the Rust fixtures.
//
// The country and region tables are large static data sets. Instead of
// translating them by hand, they are read from the Go sources with go/ast and
// written as JSON. Every extracted value is then compared with the running Go
// implementation (geoip.GetCountryInfo and geoip.IsRegionalNeighbor), so the
// generated data is verified against the real package.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/safing/portmaster/service/intel/geoip"
)

const (
	geoipCountrySource = "service/intel/geoip/country_info.go"
	geoipRegionSource  = "service/intel/geoip/regions.go"

	// Constants used in the country table.
	earthCircumferenceInKm      = 40100
	defaultCountryBasedAccuracy = 200
)

type geoipCoordinates struct {
	AccuracyRadius uint16  `json:"AccuracyRadius"`
	Latitude       float64 `json:"Latitude"`
	Longitude      float64 `json:"Longitude"`
}

type geoipContinent struct {
	Code   string `json:"Code"`
	Region string `json:"Region"`
	Name   string `json:"Name"`
}

type geoipCountry struct {
	Code      string           `json:"Code"`
	Name      string           `json:"Name"`
	Center    geoipCoordinates `json:"Center"`
	Continent geoipContinent   `json:"Continent"`
}

type geoipRegion struct {
	ID        string   `json:"ID"`
	Name      string   `json:"Name"`
	Neighbors []string `json:"Neighbors"`
}

type geoipProxLocation struct {
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  uint16  `json:"accuracy"`
	ASN       uint    `json:"asn"`
	ASOrg     string  `json:"as_org"`
	IsAnycast bool    `json:"is_anycast"`
	Satellite bool    `json:"is_satellite"`
	Anonymous bool    `json:"is_anonymous"`
}

type geoipProxCase struct {
	Name          string            `json:"name"`
	From          geoipProxLocation `json:"from"`
	To            geoipProxLocation `json:"to"`
	ProximityBits uint32            `json:"proximity_bits"`
}

type geoipPrimitiveCase struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Version  uint8  `json:"version"`
	Expected int    `json:"expected"`
}

type geoipASOrgCase struct {
	Value    string `json:"value"`
	Expected bool   `json:"expected"`
}

func genGeoIP() {
	countriesRaw := parseGoMap(geoipCountrySource, "countries")
	regionsRaw := parseGoMap(geoipRegionSource, "regions")

	// Build the final country table, applying the same transformations as the
	// package init function.
	countries := make(map[string]geoipCountry, len(countriesRaw))
	for code, raw := range countriesRaw {
		entry, ok := raw.(map[string]any)
		if !ok {
			fail(fmt.Errorf("geoip: country %s is not a struct", code))
		}

		country := geoipCountry{Code: code}
		country.Name, _ = entry["Name"].(string)
		if center, ok := entry["Center"].(map[string]any); ok {
			country.Center.AccuracyRadius = defaultCountryBasedAccuracy
			country.Center.Latitude = number(center["Latitude"])
			country.Center.Longitude = number(center["Longitude"])
		} else {
			country.Center.AccuracyRadius = defaultCountryBasedAccuracy
		}
		if continent, ok := entry["Continent"].(map[string]any); ok {
			country.Continent.Region, _ = continent["Region"].(string)
		}
		country.Continent.Code, _, _ = strings.Cut(country.Continent.Region, "-")
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
		countries[code] = country
	}

	// Verify every country against the running Go implementation.
	codes := make([]string, 0, len(countries))
	for code := range countries {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		want := countries[code]
		got := geoip.GetCountryInfo(code)
		if got.Code != want.Code ||
			got.Name != want.Name ||
			got.Center.AccuracyRadius != want.Center.AccuracyRadius ||
			got.Center.Latitude != want.Center.Latitude ||
			got.Center.Longitude != want.Center.Longitude ||
			got.Continent.Code != want.Continent.Code ||
			got.Continent.Region != want.Continent.Region ||
			got.Continent.Name != want.Continent.Name {
			fail(fmt.Errorf("geoip: country %s differs from the Go runtime:\n got  %+v\n want %+v",
				code, got, want))
		}
	}

	// Build the region table.
	regions := make(map[string]geoipRegion, len(regionsRaw))
	for id, raw := range regionsRaw {
		entry, ok := raw.(map[string]any)
		if !ok {
			fail(fmt.Errorf("geoip: region %s is not a struct", id))
		}
		region := geoipRegion{ID: id, Neighbors: []string{}}
		region.Name, _ = entry["Name"].(string)
		if neighbors, ok := entry["Neighbors"].([]any); ok {
			for _, neighbor := range neighbors {
				region.Neighbors = append(region.Neighbors, neighbor.(string))
			}
		}
		regions[id] = region
	}

	// Every continent region used by a country must exist.
	for _, code := range codes {
		region := countries[code].Continent.Region
		if region == "" {
			continue
		}
		if _, ok := regions[region]; !ok {
			fail(fmt.Errorf("geoip: country %s uses unknown region %s", code, region))
		}
	}

	// Verify the neighbor relations against the running Go implementation.
	regionIDs := make([]string, 0, len(regions))
	for id := range regions {
		regionIDs = append(regionIDs, id)
	}
	sort.Strings(regionIDs)
	locationWithRegion := func(region string) *geoip.Location {
		return &geoip.Location{
			Country: geoip.CountryInfo{
				Code:      region,
				Continent: geoip.ContinentInfo{Region: region},
			},
		}
	}
	for _, from := range regionIDs {
		for _, to := range regionIDs {
			want := false
			for _, neighbor := range regions[from].Neighbors {
				if neighbor == to {
					want = true
					break
				}
			}
			got := locationWithRegion(from).IsRegionalNeighbor(locationWithRegion(to))
			if got != want {
				fail(fmt.Errorf("geoip: neighbor %s -> %s differs: got %v want %v", from, to, got, want))
			}
		}
	}

	// Network proximity cases, computed by the Go implementation.
	makeLocation := func(spec geoipProxLocation) *geoip.Location {
		return &geoip.Location{
			Country:                      geoip.GetCountryInfo(spec.Country),
			Coordinates:                  geoip.Coordinates{AccuracyRadius: spec.Accuracy, Latitude: spec.Latitude, Longitude: spec.Longitude},
			AutonomousSystemNumber:       spec.ASN,
			AutonomousSystemOrganization: spec.ASOrg,
			IsAnycast:                    spec.IsAnycast,
			IsSatelliteProvider:          spec.Satellite,
			IsAnonymousProxy:             spec.Anonymous,
		}
	}
	proximitySpecs := []struct {
		name string
		from geoipProxLocation
		to   geoipProxLocation
	}{
		{"same-country", geoipProxLocation{Country: "DE"}, geoipProxLocation{Country: "DE"}},
		{"same-region", geoipProxLocation{Country: "DE"}, geoipProxLocation{Country: "FR"}},
		{"same-continent", geoipProxLocation{Country: "DE"}, geoipProxLocation{Country: "NO"}},
		{"different-continent", geoipProxLocation{Country: "DE"}, geoipProxLocation{Country: "JP"}},
		{"same-coordinates", geoipProxLocation{Country: "DE", Latitude: 52.52, Longitude: 13.40, Accuracy: 100}, geoipProxLocation{Country: "DE", Latitude: 52.52, Longitude: 13.40, Accuracy: 100}},
		{"nearby-coordinates", geoipProxLocation{Country: "DE", Latitude: 52.52, Longitude: 13.40, Accuracy: 100}, geoipProxLocation{Country: "DE", Latitude: 52.50, Longitude: 13.45, Accuracy: 50}},
		{"far-coordinates", geoipProxLocation{Country: "US", Latitude: 40.71, Longitude: -74.00, Accuracy: 1000}, geoipProxLocation{Country: "JP", Latitude: 35.68, Longitude: 139.69, Accuracy: 1000}},
		{"same-asn", geoipProxLocation{Country: "DE", Latitude: 52.52, Longitude: 13.40, ASN: 64500, ASOrg: "Example Network"}, geoipProxLocation{Country: "NL", Latitude: 52.37, Longitude: 4.90, ASN: 64500, ASOrg: "Example Network"}},
		{"same-as-org-only", geoipProxLocation{Country: "DE", ASN: 64501, ASOrg: "Example Network"}, geoipProxLocation{Country: "NL", ASN: 64502, ASOrg: "Example Network"}},
		{"as-org-without-asn", geoipProxLocation{Country: "DE", ASOrg: "Example Network"}, geoipProxLocation{Country: "NL", ASOrg: "Example Network"}},
		{"unknown-as-org", geoipProxLocation{Country: "DE", ASN: 64503, ASOrg: "not routed"}, geoipProxLocation{Country: "NL", ASN: 64504, ASOrg: "not routed"}},
		{"zero-coordinates", geoipProxLocation{Country: "DE"}, geoipProxLocation{Country: "DE", Latitude: 52.52, Longitude: 13.40}},
		{"default-accuracy", geoipProxLocation{Country: "DE", Latitude: 52.52, Longitude: 13.40}, geoipProxLocation{Country: "DE", Latitude: 52.60, Longitude: 13.50}},
		{"anycast", geoipProxLocation{Country: "__", IsAnycast: true}, geoipProxLocation{Country: "__", IsAnycast: true}},
	}
	proximity := make([]geoipProxCase, 0, len(proximitySpecs))
	for _, spec := range proximitySpecs {
		from := makeLocation(spec.from)
		to := makeLocation(spec.to)
		proximity = append(proximity, geoipProxCase{
			Name:          spec.name,
			From:          spec.from,
			To:            spec.to,
			ProximityBits: math.Float32bits(from.EstimateNetworkProximity(to)),
		})
	}

	// Primitive network proximity cases.
	primitiveSpecs := []struct {
		from, to string
		version  uint8
	}{
		{"1.1.1.1", "1.1.1.2", 4},
		{"8.8.8.8", "1.1.1.1", 4},
		{"1.1.1.1", "1.1.1.1", 4},
		{"2001:db8::1", "2001:db8::2", 6},
		{"2001:db8::1", "2001:db9::1", 6},
		{"1.1.1.1", "1.1.1.2", 0},
	}
	asIP := func(value string) net.IP {
		ip := net.ParseIP(value)
		if ip == nil {
			fail(fmt.Errorf("geoip: invalid IP %q", value))
		}
		return ip
	}
	primitive := make([]geoipPrimitiveCase, 0, len(primitiveSpecs))
	for _, spec := range primitiveSpecs {
		primitive = append(primitive, geoipPrimitiveCase{
			From:     spec.from,
			To:       spec.to,
			Version:  spec.version,
			Expected: geoip.PrimitiveNetworkProximity(asIP(spec.from), asIP(spec.to), spec.version),
		})
	}

	// AS organization unknown value cases.
	asOrgValues := []string{
		"", "not routed", "Not Routed", "unknown", "UNKNOWN", "nil", "null", "undef",
		"undefined", "Cloudflare, Inc.", "Example Network",
	}
	asOrg := make([]geoipASOrgCase, 0, len(asOrgValues))
	for _, value := range asOrgValues {
		asOrg = append(asOrg, geoipASOrgCase{Value: value, Expected: geoip.ASOrgUnknown(value)})
	}

	writeObject("geoip.json", map[string]any{
		"countries":           countries,
		"regions":             regions,
		"proximity":           proximity,
		"primitive_proximity": primitive,
		"as_org_unknown":      asOrg,
	})
}

// parseGoMap reads a top-level `var name = map[...]{...}` declaration from a Go
// source file and returns its literal values.
func parseGoMap(path, name string) map[string]any {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		fail(fmt.Errorf("geoip: failed to parse %s: %w", path, err))
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, ident := range valueSpec.Names {
				if ident.Name != name || index >= len(valueSpec.Values) {
					continue
				}
				literal, ok := valueSpec.Values[index].(*ast.CompositeLit)
				if !ok {
					fail(fmt.Errorf("geoip: %s is not a composite literal", name))
				}
				return mapLiteralValue(literal)
			}
		}
	}

	fail(fmt.Errorf("geoip: %s not found in %s", name, path))
	return nil
}

// mapLiteralValue evaluates a map literal with basic and struct values.
func mapLiteralValue(literal *ast.CompositeLit) map[string]any {
	out := make(map[string]any, len(literal.Elts))
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			fail(fmt.Errorf("geoip: unsupported map element %T", element))
		}
		key, ok := pair.Key.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			fail(fmt.Errorf("geoip: unsupported map key %T", pair.Key))
		}
		name, err := strconv.Unquote(key.Value)
		if err != nil {
			fail(err)
		}
		out[name] = expressionValue(pair.Value)
	}
	return out
}

// expressionValue evaluates the literal expressions used in the data tables.
func expressionValue(expr ast.Expr) any {
	switch value := expr.(type) {
	case *ast.BasicLit:
		switch value.Kind {
		case token.STRING:
			text, err := strconv.Unquote(value.Value)
			if err != nil {
				fail(err)
			}
			return text
		case token.INT:
			number, err := strconv.ParseInt(value.Value, 0, 64)
			if err != nil {
				fail(err)
			}
			return number
		case token.FLOAT:
			number, err := strconv.ParseFloat(value.Value, 64)
			if err != nil {
				fail(err)
			}
			return number
		}
	case *ast.Ident:
		switch value.Name {
		case "earthCircumferenceInKm":
			return int64(earthCircumferenceInKm)
		case "defaultCountryBasedAccuracy":
			return int64(defaultCountryBasedAccuracy)
		}
	case *ast.UnaryExpr:
		if value.Op == token.SUB {
			return -number(expressionValue(value.X))
		}
	case *ast.CompositeLit:
		// A slice literal (e.g. []string{...}).
		if _, ok := value.Type.(*ast.ArrayType); ok {
			items := make([]any, 0, len(value.Elts))
			for _, element := range value.Elts {
				items = append(items, expressionValue(element))
			}
			return items
		}
		// A struct literal, with or without an explicit type name.
		fields := make(map[string]any, len(value.Elts))
		for _, element := range value.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				fail(fmt.Errorf("geoip: unsupported struct element %T", element))
			}
			ident, ok := pair.Key.(*ast.Ident)
			if !ok {
				fail(fmt.Errorf("geoip: unsupported field name %T", pair.Key))
			}
			fields[ident.Name] = expressionValue(pair.Value)
		}
		return fields
	}

	fail(fmt.Errorf("geoip: unsupported expression %T", expr))
	return nil
}

// number converts an extracted value to float64.
func number(value any) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case int64:
		return float64(typed)
	case float64:
		return typed
	default:
		fail(fmt.Errorf("geoip: %T is not a number", value))
		return 0
	}
}
