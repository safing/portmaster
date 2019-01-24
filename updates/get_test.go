package updates

import "testing"

func testBuildVersionedFilePath(t *testing.T, identifier, version, expectedVersionedFilePath string) {
	updatesLock.Lock()
	stableUpdates[identifier] = version
	// betaUpdates[identifier] = version
	updatesLock.Unlock()

	versionedFilePath, _, _, ok := getLatestFilePath(identifier)
	if !ok {
		t.Errorf("identifier %s should exist", identifier)
	}
	if versionedFilePath != expectedVersionedFilePath {
		t.Errorf("unexpected versionedFilePath: %s", versionedFilePath)
	}
}

func TestBuildVersionedFilePath(t *testing.T) {
	testBuildVersionedFilePath(t, "path/to/asset.zip", "1.2.3", "path/to/asset_v1-2-3.zip")
	testBuildVersionedFilePath(t, "path/to/asset.tar.gz", "1.2.3b", "path/to/asset_v1-2-3b.tar.gz")
	testBuildVersionedFilePath(t, "path/to/asset", "1.2.3b", "path/to/asset_v1-2-3b")
}
