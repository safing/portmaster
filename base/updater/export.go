package updater

// Export exports the list of resources.
func (reg *ResourceRegistry) Export() map[string]*Resource {
	reg.RLock()
	defer reg.RUnlock()

	// copy the map
	copiedResources := make(map[string]*Resource)
	for key, val := range reg.resources {
		copiedResources[key] = val.Export()
	}

	return copiedResources
}
