// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"github.com/Safing/safing-core/database"

	datastore "github.com/ipfs/go-datastore"
)

// EntityClassification holds classification information about an internet entity.
type EntityClassification struct {
	lists []byte
}

// Intel holds intelligence data for a domain.
type Intel struct {
	database.Base
	Domain         string
	DomainOwner    string
	CertOwner      string
	Classification *EntityClassification
}

var intelModel *Intel // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(intelModel, func() database.Model { return new(Intel) })
}

// Create saves the Intel with the provided name in the default namespace.
func (m *Intel) Create(name string) error {
	return m.CreateObject(&database.IntelCache, name, m)
}

// CreateInNamespace saves the Intel with the provided name in the provided namespace.
func (m *Intel) CreateInNamespace(namespace *datastore.Key, name string) error {
	return m.CreateObject(namespace, name, m)
}

// Save saves the Intel.
func (m *Intel) Save() error {
	return m.SaveObject(m)
}

// getIntel fetches the Intel with the provided name in the default namespace.
func getIntel(name string) (*Intel, error) {
	return getIntelFromNamespace(&database.IntelCache, name)
}

// getIntelFromNamespace fetches the Intel with the provided name in the provided namespace.
func getIntelFromNamespace(namespace *datastore.Key, name string) (*Intel, error) {
	object, err := database.GetAndEnsureModel(namespace, name, intelModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*Intel)
	if !ok {
		return nil, database.NewMismatchError(object, intelModel)
	}
	return model, nil
}
