package endpoints

// DisplayHintEndpointList marks an option as an endpoint
// list option. It's meant to be used with DisplayHintAnnotation.
const DisplayHintEndpointList = "endpoint list"

// EndpointListAnnotation is the annotation identifier used in configuration
// options to hint the UI on available endpoint list types. If configured, only
// the specified set of entities is allowed to be used. The value is expected
// to be a single string or []string. If this annotation is missing, all
// values are expected to be allowed.
const EndpointListAnnotation = "safing/portmaster:ui:endpoint-list"

// Allowed values for the EndpointListAnnotation.
const (
	EndpointListIP               = "ip"
	EndpointListAsn              = "asn"
	EndpointListCountry          = "country"
	EndpointListDomain           = "domain"
	EndpointListIPRange          = "iprange"
	EndpointListLists            = "lists"
	EndpointListScopes           = "scopes"
	EndpointListProtocolAndPorts = "protocol-port"
)
