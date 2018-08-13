// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package netutils

import (
	"regexp"
)

var (
	// cleanDomainRegex = regexp.MustCompile("^(((?!-))(xn--)?[a-z0-9-_]{0,61}[a-z0-9]{1,1}\\.)*(xn--)?([a-z0-9-]{1,61}|[a-z0-9-]{1,30}\\.[a-z]{2,}\\.)$")
	cleanDomainRegex = regexp.MustCompile("^((xn--)?[a-z0-9-_]{0,61}[a-z0-9]{1,1}\\.)*(xn--)?([a-z0-9-]{1,61}|[a-z0-9-]{1,30}\\.[a-z]{2,}\\.)$")
)

func IsValidFqdn(fqdn string) bool {
	return cleanDomainRegex.MatchString(fqdn)
}
