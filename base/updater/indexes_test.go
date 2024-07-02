package updater

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	oldFormat = `{
	"all/ui/modules/assets.zip": "0.3.0",
	"all/ui/modules/portmaster.zip": "0.2.4",
	"linux_amd64/core/portmaster-core": "0.8.13"
}`

	newFormat = `{
	"Channel": "stable",
	"Published": "2022-01-02T00:00:00Z",
	"Releases": {
		"all/ui/modules/assets.zip": "0.3.0",
		"all/ui/modules/portmaster.zip": "0.2.4",
		"linux_amd64/core/portmaster-core": "0.8.13"
	}
}`

	formatTestChannel  = "stable"
	formatTestReleases = map[string]string{
		"all/ui/modules/assets.zip":        "0.3.0",
		"all/ui/modules/portmaster.zip":    "0.2.4",
		"linux_amd64/core/portmaster-core": "0.8.13",
	}
)

func TestIndexParsing(t *testing.T) {
	t.Parallel()

	lastRelease, err := time.Parse(time.RFC3339, "2022-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	oldIndexFile, err := ParseIndexFile([]byte(oldFormat), formatTestChannel, lastRelease)
	if err != nil {
		t.Fatal(err)
	}

	newIndexFile, err := ParseIndexFile([]byte(newFormat), formatTestChannel, lastRelease)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, formatTestChannel, oldIndexFile.Channel, "channel should be the same")
	assert.Equal(t, formatTestChannel, newIndexFile.Channel, "channel should be the same")
	assert.Equal(t, formatTestReleases, oldIndexFile.Releases, "releases should be the same")
	assert.Equal(t, formatTestReleases, newIndexFile.Releases, "releases should be the same")
}
