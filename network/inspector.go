package network

// Inspector is a connection inspection interface for detailed analysis of network connections.
type Inspector interface {
	// Name returns the name of the inspector.
	Name() string

	// Destroy cancels the inspector and frees all resources.
	// It is called as soon as Inspect returns proceed=false,
	// an error occures, or if the inspection has ended early.
	Destroy() error
}
