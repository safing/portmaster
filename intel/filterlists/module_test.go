package filterlists

/*
func TestMain(m *testing.M) {
	// we completely ignore netenv events during testing.
	ignoreNetEnvEvents.Set()

	if err := updates.DisableUpdateSchedule(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to disable update schedule: %s", err)
		os.Exit(1)
	}
	pmtesting.TestMainWithHooks(m, module, loadOnStart, nil)
}

func loadOnStart() error {
	log.SetLogLevel(log.TraceLevel)

	ch := make(chan struct{})
	defer close(ch)

	if err := updates.TriggerUpdate(); err != nil {
		return fmt.Errorf("failed to trigger update: %w", err)
	}

	var err error

	go func() {
		select {
		case <-ch:
			return

		case <-time.After(time.Minute):
			err = fmt.Errorf("timeout loading")
			close(filterListsLoaded) // let waitUntilLoaded() return
		}
	}()

	waitUntilLoaded()
	time.Sleep(time.Second * 10)
	if err != nil {
		return err
	}

	failureStatus, failureID, failureMsg := module.FailureStatus()
	if failureStatus == modules.FailureError || failureStatus == modules.FailureWarning {
		return fmt.Errorf("module in failure state: %s %q", failureID, failureMsg)
	}

	// ignore update events from now on during testing.
	ignoreUpdateEvents.Set()

	testSources := []string{"TEST"}
	testEntries := []*listEntry{
		{
			Entity:  "example.com",
			Sources: testSources,
			Type:    "Domain",
		},
		{
			Entity:  "1.1.1.1",
			Sources: testSources,
			Type:    "IPv4",
		},
		{
			Entity:  "AT",
			Sources: testSources,
			Type:    "Country",
		},
		{
			Entity:  "123",
			Sources: testSources,
			Type:    "ASN",
		},
	}

	for _, e := range testEntries {
		// add some test entries
		if err := processEntry(e); err != nil {
			return err
		}

	}

	return nil
}
*/
