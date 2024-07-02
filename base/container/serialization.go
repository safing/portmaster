package container

import (
	"encoding/json"
)

// MarshalJSON serializes the container as a JSON byte array.
func (c *Container) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.CompileData())
}

// UnmarshalJSON unserializes a container from a JSON byte array.
func (c *Container) UnmarshalJSON(data []byte) error {
	var raw []byte
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.compartments = [][]byte{raw}
	return nil
}
