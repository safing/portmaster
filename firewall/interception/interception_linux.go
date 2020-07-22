package interception

// start starts the interception.
func start() error {
	return StartNfqueueInterception()
}

// stop starts the interception.
func stop() error {
	return StopNfqueueInterception()
}
