package updater

/*
func testLoadLatestScope(t *testing.T, basePath, filePath, expectedIdentifier, expectedVersion string) {
	fullPath := filepath.Join(basePath, filePath)

	// create dir
	dirPath := filepath.Dir(fullPath)
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test dir: %s\n", err)
		return
	}

	// touch file
	err = os.WriteFile(fullPath, []byte{}, 0644)
	if err != nil {
		t.Fatalf("could not create test file: %s\n", err)
		return
	}

	// run loadLatestScope
	latest, err := ScanForLatest(basePath, true)
	if err != nil {
		t.Errorf("could not update latest: %s\n", err)
		return
	}
	for key, val := range latest {
		localUpdates[key] = val
	}

	// test result
	version, ok := localUpdates[expectedIdentifier]
	if !ok {
		t.Errorf("identifier %s not in map", expectedIdentifier)
		t.Errorf("current map: %v", localUpdates)
	}
	if version != expectedVersion {
		t.Errorf("unexpected version for %s: %s", filePath, version)
	}
}

func TestLoadLatestScope(t *testing.T) {

	updatesLock.Lock()
	defer updatesLock.Unlock()

	tmpDir, err := os.MkdirTemp("", "testing_")
	if err != nil {
		t.Fatalf("could not create test dir: %s\n", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	testLoadLatestScope(t, tmpDir, "all/ui/assets_v1-2-3.zip", "all/ui/assets.zip", "1.2.3")
	testLoadLatestScope(t, tmpDir, "all/ui/assets_v1-2-4b.zip", "all/ui/assets.zip", "1.2.4b")
	testLoadLatestScope(t, tmpDir, "all/ui/assets_v1-2-5.zip", "all/ui/assets.zip", "1.2.5")
	testLoadLatestScope(t, tmpDir, "all/ui/assets_v1-3-4.zip", "all/ui/assets.zip", "1.3.4")
	testLoadLatestScope(t, tmpDir, "all/ui/assets_v2-3-4.zip", "all/ui/assets.zip", "2.3.4")
	testLoadLatestScope(t, tmpDir, "all/ui/assets_v1-2-3.zip", "all/ui/assets.zip", "2.3.4")
	testLoadLatestScope(t, tmpDir, "all/ui/assets_v1-2-4.zip", "all/ui/assets.zip", "2.3.4")
	testLoadLatestScope(t, tmpDir, "all/ui/assets_v1-3-4.zip", "all/ui/assets.zip", "2.3.4")
	testLoadLatestScope(t, tmpDir, "os_platform/portmaster/portmaster_v1-2-3", "os_platform/portmaster/portmaster", "1.2.3")
	testLoadLatestScope(t, tmpDir, "os_platform/portmaster/portmaster_v2-1-1", "os_platform/portmaster/portmaster", "2.1.1")
	testLoadLatestScope(t, tmpDir, "os_platform/portmaster/portmaster_v1-2-3", "os_platform/portmaster/portmaster", "2.1.1")

}
*/
