package netenv

import (
	"errors"

	"github.com/safing/portmaster/base/api"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "network/gateways",
		Read: api.PermitUser,
		StructFunc: func(ar *api.Request) (i interface{}, err error) {
			return Gateways(), nil
		},
		Name:        "Get Default Gateways",
		Description: "Returns the current active default gateways of the network.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "network/nameservers",
		Read: api.PermitUser,
		StructFunc: func(ar *api.Request) (i interface{}, err error) {
			return Nameservers(), nil
		},
		Name:        "Get System Nameservers",
		Description: "Returns the currently configured nameservers on the OS.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "network/location",
		Read: api.PermitUser,
		StructFunc: func(ar *api.Request) (i interface{}, err error) {
			locs, ok := GetInternetLocation()
			if ok {
				return locs, nil
			}
			return nil, errors.New("no location data available")
		},
		Name:        "Get Approximate Internet Location",
		Description: "Returns an approximation of where the device is on the Internet.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "network/location/traceroute",
		Read: api.PermitUser,
		StructFunc: func(ar *api.Request) (i interface{}, err error) {
			return getLocationFromTraceroute(&DeviceLocations{})
		},
		Name:        "Get Approximate Internet Location via Traceroute",
		Description: "Returns an approximation of where the device is on the Internet using a the traceroute technique.",
	}); err != nil {
		return err
	}

	return nil
}
