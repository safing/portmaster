package main

func RunTest(name string, fn func() error) {
	result := &checkResult{
		name: name,
	}

	resultsLock.Lock()
	results = append(results, result)
	resultsLock.Unlock()

	createTestRunNotification()

	if err := fn(); err != nil {
		result.err = err
	} else {
		result.passed = true
	}

	createTestRunNotification()
}
