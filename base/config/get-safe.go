package config

type safe struct{}

// Concurrent makes concurrency safe get methods available.
// The regular GetAs* functions are concurrency safe as well; these methods are
// kept for compatibility and share the same lock-free implementation.
var Concurrent = &safe{}

// GetAsString returns a function that returns the wanted string with high performance.
func (cs *safe) GetAsString(name string, fallback string) StringOption {
	return GetAsString(name, fallback)
}

// GetAsStringArray returns a function that returns the wanted string array with high performance.
func (cs *safe) GetAsStringArray(name string, fallback []string) StringArrayOption {
	return GetAsStringArray(name, fallback)
}

// GetAsInt returns a function that returns the wanted int with high performance.
func (cs *safe) GetAsInt(name string, fallback int64) IntOption {
	return GetAsInt(name, fallback)
}

// GetAsBool returns a function that returns the wanted bool with high performance.
func (cs *safe) GetAsBool(name string, fallback bool) BoolOption {
	return GetAsBool(name, fallback)
}
