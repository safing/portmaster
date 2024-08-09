package geoip

import (
	"github.com/safing/portmaster/base/utils"
)

// IsRegionalNeighbor returns whether the supplied location is a regional neighbor.
func (l *Location) IsRegionalNeighbor(other *Location) bool {
	if l.Country.Continent.Region == "" || other.Country.Continent.Region == "" {
		return false
	}
	if region, ok := regions[l.Country.Continent.Region]; ok {
		return utils.StringInSlice(region.Neighbors, other.Country.Continent.Region)
	}
	return false
}

// Region defines a geographic region and neighboring regions.
type Region struct {
	ID        string
	Name      string
	Neighbors []string
}

var regions = map[string]*Region{
	"AF-C": {
		ID:   "AF-C",
		Name: "Africa, Sub-Saharan Africa, Middle Africa",
		Neighbors: []string{
			"AF-E",
			"AF-N",
			"AF-S",
			"AF-W",
		},
	},
	"AF-E": {
		ID:   "AF-E",
		Name: "Africa, Sub-Saharan Africa, Eastern Africa",
		Neighbors: []string{
			"AF-C",
			"AF-N",
			"AF-S",
		},
	},
	"AF-N": {
		ID:   "AF-N",
		Name: "Africa, Northern Africa",
		Neighbors: []string{
			"AF-C",
			"AF-E",
			"AF-W",
			"AS-W",
			"EU-S",
		},
	},
	"AF-S": {
		ID:   "AF-S",
		Name: "Africa, Sub-Saharan Africa, Southern Africa",
		Neighbors: []string{
			"AF-C",
			"AF-E",
			"AF-W",
		},
	},
	"AF-W": {
		ID:   "AF-W",
		Name: "Africa, Sub-Saharan Africa, Western Africa",
		Neighbors: []string{
			"AF-C",
			"AF-N",
			"AF-S",
		},
	},
	"AN": {
		ID:        "AN",
		Name:      "Antarctica",
		Neighbors: []string{},
	},
	"AS-C": {
		ID:   "AS-C",
		Name: "Asia, Central Asia",
		Neighbors: []string{
			"AS-E",
			"AS-S",
			"AS-SE",
			"AS-W",
		},
	},
	"AS-E": {
		ID:   "AS-E",
		Name: "Asia, Eastern Asia",
		Neighbors: []string{
			"AS-C",
			"AS-S",
			"AS-SE",
		},
	},
	"AS-S": {
		ID:   "AS-S",
		Name: "Asia, Southern Asia",
		Neighbors: []string{
			"AS-C",
			"AS-E",
			"AS-SE",
			"AS-W",
		},
	},
	"AS-SE": {
		ID:   "AS-SE",
		Name: "Asia, South-eastern Asia",
		Neighbors: []string{
			"AS-C",
			"AS-E",
			"AS-S",
			"OC-C",
			"OC-E",
			"OC-N",
			"OC-S",
		},
	},
	"AS-W": {
		ID:   "AS-W",
		Name: "Asia, Western Asia",
		Neighbors: []string{
			"AF-N",
			"AS-C",
			"AS-S",
			"EU-E",
		},
	},
	"EU-E": {
		ID:   "EU-E",
		Name: "Europe, Eastern Europe",
		Neighbors: []string{
			"AS-W",
			"EU-N",
			"EU-S",
			"EU-W",
		},
	},
	"EU-N": {
		ID:   "EU-N",
		Name: "Europe, Northern Europe",
		Neighbors: []string{
			"EU-E",
			"EU-S",
			"EU-W",
		},
	},
	"EU-S": {
		ID:   "EU-S",
		Name: "Europe, Southern Europe",
		Neighbors: []string{
			"AF-N",
			"EU-E",
			"EU-N",
			"EU-W",
		},
	},
	"EU-W": {
		ID:   "EU-W",
		Name: "Europe, Western Europe",
		Neighbors: []string{
			"EU-E",
			"EU-N",
			"EU-S",
		},
	},
	"NA-E": {
		ID:   "NA-E",
		Name: "North America, Caribbean",
		Neighbors: []string{
			"NA-N",
			"NA-S",
			"SA",
		},
	},
	"NA-N": {
		ID:   "NA-N",
		Name: "North America, Northern America",
		Neighbors: []string{
			"NA-E",
			"NA-N",
			"NA-S",
		},
	},
	"NA-S": {
		ID:   "NA-S",
		Name: "North America, Central America",
		Neighbors: []string{
			"NA-E",
			"NA-N",
			"NA-S",
			"SA",
		},
	},
	"OC-C": {
		ID:   "OC-C",
		Name: "Oceania, Melanesia",
		Neighbors: []string{
			"AS-SE",
			"OC-E",
			"OC-N",
			"OC-S",
		},
	},
	"OC-E": {
		ID:   "OC-E",
		Name: "Oceania, Polynesia",
		Neighbors: []string{
			"AS-SE",
			"OC-C",
			"OC-N",
			"OC-S",
		},
	},
	"OC-N": {
		ID:   "OC-N",
		Name: "Oceania, Micronesia",
		Neighbors: []string{
			"AS-SE",
			"OC-C",
			"OC-E",
			"OC-S",
		},
	},
	"OC-S": {
		ID:   "OC-S",
		Name: "Oceania, Australia and New Zealand",
		Neighbors: []string{
			"AS-SE",
			"OC-C",
			"OC-E",
			"OC-N",
		},
	},
	"SA": { // TODO: Split up
		ID:   "SA",
		Name: "South America",
		Neighbors: []string{
			"NA-E",
			"NA-S",
		},
	},
}
