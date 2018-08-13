// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"strings"

	"github.com/Safing/safing-core/database"

	datastore "github.com/ipfs/go-datastore"
)

// IPInfo represents various information about an IP.
type IPInfo struct {
	database.Base
	Domains []string
}

var ipInfoModel *IPInfo // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(ipInfoModel, func() database.Model { return new(IPInfo) })
}

// Create saves the IPInfo with the provided name in the default namespace.
func (m *IPInfo) Create(name string) error {
	return m.CreateObject(&database.IPInfoCache, name, m)
}

// CreateInNamespace saves the IPInfo with the provided name in the provided namespace.
func (m *IPInfo) CreateInNamespace(namespace *datastore.Key, name string) error {
	return m.CreateObject(namespace, name, m)
}

// Save saves the IPInfo.
func (m *IPInfo) Save() error {
	return m.SaveObject(m)
}

// GetIPInfo fetches the IPInfo with the provided name in the default namespace.
func GetIPInfo(name string) (*IPInfo, error) {
	return GetIPInfoFromNamespace(&database.IPInfoCache, name)
}

// GetIPInfoFromNamespace fetches the IPInfo with the provided name in the provided namespace.
func GetIPInfoFromNamespace(namespace *datastore.Key, name string) (*IPInfo, error) {
	object, err := database.GetAndEnsureModel(namespace, name, ipInfoModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*IPInfo)
	if !ok {
		return nil, database.NewMismatchError(object, ipInfoModel)
	}
	return model, nil
}

// FmtDomains returns a string consisting of the domains that have seen to use this IP, joined by " or "
func (m *IPInfo) FmtDomains() string {
	return strings.Join(m.Domains, " or ")
}
