//! Texts of the DNS options, copied verbatim from Go `service/resolver/config.go`.

/// Help of the nameservers option; double quotes are replaced by backticks at registration.
pub(crate) const NAMESERVERS_HELP_RAW: &str = r####"DNS servers are used in the order as entered. The first one will be used as the primary DNS Server. Only if it fails, will the other servers be used as a fallback - in their respective order. If all fail, or if no DNS Server is configured here, the Portmaster will use the one configured in your system or network.

Additionally, if it is more likely that the DNS server of your system or network has a (better) answer to a request, they will be asked first. This will be the case for special local domains and domain spaces announced on the current network.

DNS servers are configured in a URL format. This allows you to specify special settings for a resolver. If you just want to use a resolver at IP 10.2.3.4, please enter: "dns://10.2.3.4"  
The format is: "protocol://host:port?parameter=value&parameter=value"  

For DoH servers, you can also just paste the URL given by the DNS provider.  
When referring to the DNS server using a domain name, as with DoH, it is highly recommended to also specify the IP address using the "ip" parameter, so Portmaster does not have to resolve it.

- Protocol
	- "dot": DNS-over-TLS (or "tls"; recommended)  
	- "doh": DNS-over-HTTPS (or "https")
	- "dns": plain old DNS  
	- "tcp": plain old DNS over TCP
- Host: specify the domain or IP of the resolver
- Port: optionally define a custom port
- Parameters:
	- "name": give your DNS Server a name that is used for messages and logs
	- "verify": domain name to verify for "dot", only valid for "dot" and "doh"
	- "ip": IP address (if using a domain), so Portmaster does not need to resolve it using the system resolver - this is highly recommended
	- "blockedif": detect if the name server blocks a query, options:
		- "empty": server replies with NXDomain status, but without any other record in any section
		- "refused": server replies with Refused status
		- "zeroip": server replies with an IP address, but it is zero
	- "search": specify prioritized domains/TLDs for this resolver (delimited by ",")
	- "search-only": use this resolver for domains in the "search" parameter only (no value)
"####;
