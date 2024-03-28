package endpoints

// DisplayHintEndpointList marks an option as an endpoint
// list option. It's meant to be used with DisplayHintAnnotation.
const DisplayHintEndpointList = "endpoint list"

// EndpointListVerdictNamesAnnotation is the annotation identifier used in
// configuration options to hint the UI on names to be used for endpoint list
// verdicts.
// If configured, it must be of type map[string]string, mapping the verdict
// symbol to a name to be displayed in the UI.
// May only used when config.DisplayHintAnnotation is set to DisplayHintEndpointList.
const EndpointListVerdictNamesAnnotation = "safing/portmaster:ui:endpoint-list:verdict-names"
