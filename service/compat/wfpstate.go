package compat

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/safing/portmaster/base/utils/osdetail"
)

// GetWFPState queries the system for the WFP state and returns a simplified
// and cleaned version.
func GetWFPState() (*SimplifiedWFPState, error) {
	// Use a file to get the wfp state, as the terminal isn't able to return the
	// data encoded in UTF-8.
	tmpDir, err := os.MkdirTemp("", "portmaster-debug-data-wfpstate")
	if err != nil {
		return nil, fmt.Errorf("failed to create tmp dir for wfpstate: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()
	tmpFile := filepath.Join(tmpDir, "wfpstate.xml")

	// Get wfp state and write it to the tmp file.
	_, err = osdetail.RunCmd(
		"netsh.exe",
		"wfp",
		"show",
		"state",
		tmpFile,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to write wfp state to tmp file: %w", err)
	}

	// Get tmp file contents.
	output, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read wfp state to tmp file: %w", err)
	}
	if len(output) == 0 {
		return nil, errors.New("wfp state tmp file was empty")
	}

	// Parse wfp state.
	parsedState, err := parseWFPState(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse wfpstate: %w", err)
	}

	// Return simplified and cleaned state.
	return parsedState.simplified(), nil
}

/*
Interesting data is found at:

providers->item[]
	->displayData->name
	->displayData->description
	->providerKey

subLayers->item[]
	->displayData->name
	->displayData->description
	->subLayerKey

layers->item[]->callouts->item[]
	->displayData->name
	->displayData->description
	->calloutKey
	->providerKey
	->applicableLayer

layers->item[]->filters->item[]
	->displayData->name
	->displayData->description
	->filterKey
	->providerKey
	->layerKey
	->subLayerKey
*/

// SimplifiedWFPState is a simplified version of the full WFP state.
type SimplifiedWFPState struct {
	Providers []*WFPProvider
	SubLayers []*WFPSubLayer
	Callouts  []*WFPCallout
	Filters   []*WFPFilter
}

// WFPProvider represents a WFP Provider.
type WFPProvider struct {
	Name        string
	Description string
	ProviderKey string
}

// WFPSubLayer represents a WFP SubLayer.
type WFPSubLayer struct {
	Name        string
	Description string
	SubLayerKey string
}

// WFPCallout represents a WFP Callout.
type WFPCallout struct {
	Name            string
	Description     string
	CalloutKey      string
	ProviderKey     string
	ApplicableLayer string
}

// WFPFilter represents a WFP Filter.
type WFPFilter struct {
	Name        string
	Description string
	FilterKey   string
	ProviderKey string
	LayerKey    string
	SubLayerKey string
}

// Keys returns all keys found in the WFP state.
func (sw *SimplifiedWFPState) Keys() map[string]struct{} {
	lookupMap := make(map[string]struct{}, len(sw.Providers)+len(sw.SubLayers)+len(sw.Callouts)+len(sw.Filters))

	// Collect keys.
	for _, provider := range sw.Providers {
		lookupMap[provider.ProviderKey] = struct{}{}
	}
	for _, subLayer := range sw.SubLayers {
		lookupMap[subLayer.SubLayerKey] = struct{}{}
	}
	for _, callout := range sw.Callouts {
		lookupMap[callout.CalloutKey] = struct{}{}
	}
	for _, filter := range sw.Filters {
		lookupMap[filter.FilterKey] = struct{}{}
	}

	return lookupMap
}

// AsTable formats the simplified WFP state as a table.
func (sw *SimplifiedWFPState) AsTable() string {
	rows := make([]string, 0, len(sw.Providers)+len(sw.SubLayers)+len(sw.Callouts)+len(sw.Filters))

	// Collect data and put it into rows.
	for _, provider := range sw.Providers {
		rows = append(rows, strings.Join([]string{
			provider.Name,
			"Provider",
			provider.Description,
			provider.ProviderKey,
		}, "\t"))
	}
	for _, subLayer := range sw.SubLayers {
		rows = append(rows, strings.Join([]string{
			subLayer.Name,
			"SubLayer",
			subLayer.Description,
			subLayer.SubLayerKey,
		}, "\t"))
	}
	for _, callout := range sw.Callouts {
		rows = append(rows, strings.Join([]string{
			callout.Name,
			"Callout",
			callout.Description,
			callout.CalloutKey,
			callout.ProviderKey,
			callout.ApplicableLayer,
		}, "\t"))
	}
	for _, filter := range sw.Filters {
		rows = append(rows, strings.Join([]string{
			filter.Name,
			"Filter",
			filter.Description,
			filter.FilterKey,
			filter.ProviderKey,
			filter.LayerKey,
			filter.SubLayerKey,
		}, "\t"))
	}

	// Sort and build table.
	sort.Strings(rows)
	buf := bytes.NewBuffer(nil)
	tabWriter := tabwriter.NewWriter(buf, 8, 4, 3, ' ', 0)
	for _, row := range rows {
		fmt.Fprint(tabWriter, row)
		fmt.Fprint(tabWriter, "\n")
	}
	_ = tabWriter.Flush()

	return buf.String()
}

// wfpState is the WFP state as returned by `netsh.exe wfp show state -`.
type wfpState struct {
	XMLName   xml.Name `xml:"wfpstate"`
	Text      string   `xml:",chardata"`
	TimeStamp string   `xml:"timeStamp"`
	Providers struct {
		Text     string `xml:",chardata"`
		NumItems string `xml:"numItems,attr"`
		Item     []struct {
			Text        string `xml:",chardata"`
			ProviderKey string `xml:"providerKey"`
			DisplayData struct {
				Text        string `xml:",chardata"`
				Name        string `xml:"name"`
				Description string `xml:"description"`
			} `xml:"displayData"`
			Flags struct {
				Text     string `xml:",chardata"`
				NumItems string `xml:"numItems,attr"`
				Item     string `xml:"item"`
			} `xml:"flags"`
			ProviderData string `xml:"providerData"`
			ServiceName  string `xml:"serviceName"`
		} `xml:"item"`
	} `xml:"providers"`
	SubLayers struct {
		Text     string `xml:",chardata"`
		NumItems string `xml:"numItems,attr"`
		Item     []struct {
			Text        string `xml:",chardata"`
			SubLayerKey string `xml:"subLayerKey"`
			DisplayData struct {
				Text        string `xml:",chardata"`
				Name        string `xml:"name"`
				Description string `xml:"description"`
			} `xml:"displayData"`
			Flags struct {
				Text     string `xml:",chardata"`
				NumItems string `xml:"numItems,attr"`
				Item     string `xml:"item"`
			} `xml:"flags"`
			ProviderKey  string `xml:"providerKey"`
			ProviderData string `xml:"providerData"`
			Weight       string `xml:"weight"`
		} `xml:"item"`
	} `xml:"subLayers"`
	Layers struct {
		Text     string `xml:",chardata"`
		NumItems string `xml:"numItems,attr"`
		Item     []struct {
			Text  string `xml:",chardata"`
			Layer struct {
				Text        string `xml:",chardata"`
				LayerKey    string `xml:"layerKey"`
				DisplayData struct {
					Text        string `xml:",chardata"`
					Name        string `xml:"name"`
					Description string `xml:"description"`
				} `xml:"displayData"`
				Flags struct {
					Text     string   `xml:",chardata"`
					NumItems string   `xml:"numItems,attr"`
					Item     []string `xml:"item"`
				} `xml:"flags"`
				Field struct {
					Text     string `xml:",chardata"`
					NumItems string `xml:"numItems,attr"`
					Item     []struct {
						Text     string `xml:",chardata"`
						FieldKey string `xml:"fieldKey"`
						Type     string `xml:"type"`
						DataType string `xml:"dataType"`
					} `xml:"item"`
				} `xml:"field"`
				DefaultSubLayerKey string `xml:"defaultSubLayerKey"`
				LayerID            string `xml:"layerId"`
			} `xml:"layer"`
			Callouts struct {
				Text     string `xml:",chardata"`
				NumItems string `xml:"numItems,attr"`
				Item     []struct {
					Text        string `xml:",chardata"`
					CalloutKey  string `xml:"calloutKey"`
					DisplayData struct {
						Text        string `xml:",chardata"`
						Name        string `xml:"name"`
						Description string `xml:"description"`
					} `xml:"displayData"`
					Flags struct {
						Text     string   `xml:",chardata"`
						NumItems string   `xml:"numItems,attr"`
						Item     []string `xml:"item"`
					} `xml:"flags"`
					ProviderKey     string `xml:"providerKey"`
					ProviderData    string `xml:"providerData"`
					ApplicableLayer string `xml:"applicableLayer"`
					CalloutID       string `xml:"calloutId"`
				} `xml:"item"`
			} `xml:"callouts"`
			Filters struct {
				Text     string `xml:",chardata"`
				NumItems string `xml:"numItems,attr"`
				Item     []struct {
					Text        string `xml:",chardata"`
					FilterKey   string `xml:"filterKey"`
					DisplayData struct {
						Text        string `xml:",chardata"`
						Name        string `xml:"name"`
						Description string `xml:"description"`
					} `xml:"displayData"`
					Flags struct {
						Text     string   `xml:",chardata"`
						NumItems string   `xml:"numItems,attr"`
						Item     []string `xml:"item"`
					} `xml:"flags"`
					ProviderKey  string `xml:"providerKey"`
					ProviderData struct {
						Text     string `xml:",chardata"`
						Data     string `xml:"data"`
						AsString string `xml:"asString"`
					} `xml:"providerData"`
					LayerKey    string `xml:"layerKey"`
					SubLayerKey string `xml:"subLayerKey"`
					Weight      struct {
						Text   string `xml:",chardata"`
						Type   string `xml:"type"`
						Uint8  string `xml:"uint8"`
						Uint64 string `xml:"uint64"`
					} `xml:"weight"`
					FilterCondition struct {
						Text     string `xml:",chardata"`
						NumItems string `xml:"numItems,attr"`
						Item     []struct {
							Text           string `xml:",chardata"`
							FieldKey       string `xml:"fieldKey"`
							MatchType      string `xml:"matchType"`
							ConditionValue struct {
								Text       string `xml:",chardata"`
								Type       string `xml:"type"`
								Uint32     string `xml:"uint32"`
								Uint16     string `xml:"uint16"`
								RangeValue struct {
									Text     string `xml:",chardata"`
									ValueLow struct {
										Text        string `xml:",chardata"`
										Type        string `xml:"type"`
										Uint16      string `xml:"uint16"`
										Uint32      string `xml:"uint32"`
										ByteArray16 string `xml:"byteArray16"`
									} `xml:"valueLow"`
									ValueHigh struct {
										Text        string `xml:",chardata"`
										Type        string `xml:"type"`
										Uint16      string `xml:"uint16"`
										Uint32      string `xml:"uint32"`
										ByteArray16 string `xml:"byteArray16"`
									} `xml:"valueHigh"`
								} `xml:"rangeValue"`
								Uint8    string `xml:"uint8"`
								ByteBlob struct {
									Text     string `xml:",chardata"`
									Data     string `xml:"data"`
									AsString string `xml:"asString"`
								} `xml:"byteBlob"`
								Sd     string `xml:"sd"`
								Sid    string `xml:"sid"`
								Uint64 string `xml:"uint64"`
							} `xml:"conditionValue"`
						} `xml:"item"`
					} `xml:"filterCondition"`
					Action struct {
						Text       string `xml:",chardata"`
						Type       string `xml:"type"`
						FilterType string `xml:"filterType"`
					} `xml:"action"`
					RawContext      string `xml:"rawContext"`
					Reserved        string `xml:"reserved"`
					FilterID        string `xml:"filterId"`
					EffectiveWeight struct {
						Text   string `xml:",chardata"`
						Type   string `xml:"type"`
						Uint64 string `xml:"uint64"`
					} `xml:"effectiveWeight"`
					ProviderContextKey string `xml:"providerContextKey"`
				} `xml:"item"`
			} `xml:"filters"`
		} `xml:"item"`
	} `xml:"layers"`
}

func parseWFPState(data []byte) (*wfpState, error) {
	w := &wfpState{}
	err := xml.Unmarshal(data, w)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (w *wfpState) simplified() *SimplifiedWFPState {
	sw := &SimplifiedWFPState{
		Providers: make([]*WFPProvider, 0, len(w.Providers.Item)),
		SubLayers: make([]*WFPSubLayer, 0, len(w.SubLayers.Item)),
		Callouts:  make([]*WFPCallout, 0, len(w.Layers.Item)),
		Filters:   make([]*WFPFilter, 0, len(w.Layers.Item)),
	}

	// Collect data.
	for _, provider := range w.Providers.Item {
		if isIgnoredProvider(provider.DisplayData.Name, provider.ProviderKey) {
			continue
		}

		sw.Providers = append(sw.Providers, &WFPProvider{
			Name:        defaultTo(provider.DisplayData.Name, "[no name]"),
			Description: defaultTo(provider.DisplayData.Description, "[no description]"),
			ProviderKey: defaultTo(provider.ProviderKey, "[no provider key]"),
		})
	}
	for _, subLayer := range w.SubLayers.Item {
		if isIgnoredProvider(subLayer.DisplayData.Name, "") {
			continue
		}

		sw.SubLayers = append(sw.SubLayers, &WFPSubLayer{
			Name:        defaultTo(subLayer.DisplayData.Name, "[no name]"),
			Description: defaultTo(subLayer.DisplayData.Description, "[no description]"),
			SubLayerKey: defaultTo(subLayer.SubLayerKey, "[no sublayer key]"),
		})
	}
	for _, layer := range w.Layers.Item {
		for _, callout := range layer.Callouts.Item {
			if isIgnoredProvider(callout.DisplayData.Name, callout.ProviderKey) {
				continue
			}

			sw.Callouts = append(sw.Callouts, &WFPCallout{
				Name:            defaultTo(callout.DisplayData.Name, "[no name]"),
				Description:     defaultTo(callout.DisplayData.Description, "[no description]"),
				CalloutKey:      defaultTo(callout.CalloutKey, "[no callout key]"),
				ProviderKey:     defaultTo(callout.ProviderKey, "[no provider key]"),
				ApplicableLayer: defaultTo(callout.ApplicableLayer, "[no applicable layer]"),
			})
		}
		for _, filter := range layer.Filters.Item {
			if isIgnoredProvider(filter.DisplayData.Name, filter.ProviderKey) {
				continue
			}

			sw.Filters = append(sw.Filters, &WFPFilter{
				Name:        defaultTo(filter.DisplayData.Name, "[no name]"),
				Description: defaultTo(filter.DisplayData.Description, "[no description]"),
				FilterKey:   defaultTo(filter.FilterKey, "[no filter key]"),
				ProviderKey: defaultTo(filter.ProviderKey, "[no provider key]"),
				LayerKey:    defaultTo(filter.LayerKey, "[no layer key]"),
				SubLayerKey: defaultTo(filter.SubLayerKey, "[no sublayer key]"),
			})
		}
	}

	return sw
}

func isIgnoredProvider(name, key string) bool {
	// Check provider key.
	if key != "" {
		matched := true
		switch key {
		case "{1bebc969-61a5-4732-a177-847a0817862a}": // Microsoft Windows Defender Firewall IPsec Provider.
		case "{4b153735-1049-4480-aab4-d1b9bdc03710}": // Microsoft Windows Defender Firewall Provider.
		case "{893a4f22-9bba-49b7-8c66-3d40929c8fd5}": // Microsoft Windows Teredo firewall provider.
		case "{8e44982a-f477-11df-85ce-78e7d1810190}": // Windows Network Data Usage (NDU) Provider.
		case "{9c2532b4-0314-434f-8274-0cbaebdbda56}": // Microsoft Windows edge traversal socket option authorization provider.
		case "{aa6a7d87-7f8f-4d2a-be53-fda555cd5fe3}": // Microsoft Windows Defender Firewall IPsec Provider.
		case "{c698301d-9129-450c-937c-f4b834bfb374}": // Microsoft Windows edge traversal socket option authorization provider.
		case "{decc16ca-3f33-4346-be1e-8fb4ae0f3d62}": // Microsoft Windows Defender Firewall Provider.
		case "FWPM_PROVIDER_IKEEXT": // Microsoft Windows WFP Built-in IKEEXT provider used to identify filters added by IKE/AuthIP.
		case "FWPM_PROVIDER_IPSEC_DOSP_CONFIG": // Microsoft Windows WFP Built-in IPsec DoS Protection configuration provider used to identify filters added by IPsec Denial of Service Protection.
		case "FWPM_PROVIDER_MPSSVC_APP_ISOLATION": // Microsoft Windows WFP Built-in MPSSVC App Isolation provider.
		case "FWPM_PROVIDER_MPSSVC_EDP": // Microsoft Windows WFP Built-in MPSSVC Enterprise Data Protection provider.
		case "FWPM_PROVIDER_MPSSVC_TENANT_RESTRICTIONS": // Microsoft Windows WFP Built-in MPSSVC Tenant Restrictions provider.
		case "FWPM_PROVIDER_MPSSVC_WF": // Microsoft Windows WFP Built-in MPSSVC Windows Firewall provider.
		case "FWPM_PROVIDER_MPSSVC_WSH": // Microsoft Windows WFP Built-in MPSSVC Windows Service Hardening and Quarantine provider.
		case "FWPM_PROVIDER_TCP_CHIMNEY_OFFLOAD": // Microsoft Windows WFP Built-in TCP Chimney Offload provider used to identify filters added by TCP Chimney Offload.
		case "FWPM_PROVIDER_TCP_TEMPLATES": // Microsoft Windows WFP Built-in TCP Templates provider used to identify filters added by TCP Template based configuration.
		default:
			matched = false
		}
		if matched {
			return true
		}
	}

	// Some entries don't have a provider key (set).
	// These are pretty generic, but the output strings are localized.
	if name != "" {
		switch {
		case strings.Contains(name, "Microsoft Corporation"):
			return true
		case strings.Contains(name, "windefend"):
			return true
		case strings.Contains(name, "WFP"):
			return true
		case strings.Contains(name, "RPC"):
			return true
		case strings.Contains(name, "NDU"):
			return true
		}
	}

	return false
}

func defaultTo(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
