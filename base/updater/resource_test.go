package updater

import (
	"fmt"
	"testing"

	semver "github.com/hashicorp/go-version"
	"github.com/stretchr/testify/assert"
)

func TestVersionSelection(t *testing.T) {
	t.Parallel()

	res := registry.newResource("test/a")

	err := res.AddVersion("1.2.2", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	err = res.AddVersion("1.2.3", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	err = res.AddVersion("1.2.4-beta", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	err = res.AddVersion("1.2.4-staging", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	err = res.AddVersion("1.2.5", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	err = res.AddVersion("1.2.6-beta", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	err = res.AddVersion("0", true, false, false)
	if err != nil {
		t.Fatal(err)
	}

	registry.UsePreReleases = true
	registry.DevMode = true
	registry.Online = true
	res.Index = &Index{AutoDownload: true}

	res.selectVersion()
	if res.SelectedVersion.VersionNumber != "0.0.0" {
		t.Errorf("selected version should be 0.0.0, not %s", res.SelectedVersion.VersionNumber)
	}

	registry.DevMode = false
	res.selectVersion()
	if res.SelectedVersion.VersionNumber != "1.2.6-beta" {
		t.Errorf("selected version should be 1.2.6-beta, not %s", res.SelectedVersion.VersionNumber)
	}

	registry.UsePreReleases = false
	res.selectVersion()
	if res.SelectedVersion.VersionNumber != "1.2.5" {
		t.Errorf("selected version should be 1.2.5, not %s", res.SelectedVersion.VersionNumber)
	}

	registry.Online = false
	res.selectVersion()
	if res.SelectedVersion.VersionNumber != "1.2.3" {
		t.Errorf("selected version should be 1.2.3, not %s", res.SelectedVersion.VersionNumber)
	}

	f123 := res.GetFile()
	f123.markActiveWithLocking()

	err = res.Blacklist("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if res.SelectedVersion.VersionNumber != "1.2.2" {
		t.Errorf("selected version should be 1.2.2, not %s", res.SelectedVersion.VersionNumber)
	}

	if !f123.UpgradeAvailable() {
		t.Error("upgrade should be available (flag)")
	}
	select {
	case <-f123.WaitForAvailableUpgrade():
	default:
		t.Error("upgrade should be available (chan)")
	}

	t.Logf("resource: %+v", res)
	for _, rv := range res.Versions {
		t.Logf("version %s: %+v", rv.VersionNumber, rv)
	}
}

func TestVersionParsing(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.2.3", parseVersion("1.2.3"))
	assert.Equal(t, "1.2.0", parseVersion("1.2.0"))
	assert.Equal(t, "0.2.0", parseVersion("0.2.0"))
	assert.Equal(t, "0.0.0", parseVersion("0"))
	assert.Equal(t, "1.2.3-b", parseVersion("1.2.3-b"))
	assert.Equal(t, "1.2.3-b", parseVersion("1.2.3b"))
	assert.Equal(t, "1.2.3-beta", parseVersion("1.2.3-beta"))
	assert.Equal(t, "1.2.3-beta", parseVersion("1.2.3beta"))
	assert.Equal(t, "1.2.3", parseVersion("01.02.03"))
}

func parseVersion(v string) string {
	sv, err := semver.NewVersion(v)
	if err != nil {
		return fmt.Sprintf("failed to parse version: %s", err)
	}
	return sv.String()
}
