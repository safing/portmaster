package binmeta

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateBinaryNameFromPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Nslookup", GenerateBinaryNameFromPath("nslookup.exe"))
	assert.Equal(t, "System Settings", GenerateBinaryNameFromPath("SystemSettings.exe"))
	assert.Equal(t, "One Drive Setup", GenerateBinaryNameFromPath("OneDriveSetup.exe"))
	assert.Equal(t, "Msedge", GenerateBinaryNameFromPath("msedge.exe"))
	assert.Equal(t, "SIH Client", GenerateBinaryNameFromPath("SIHClient.exe"))
	assert.Equal(t, "Openvpn Gui", GenerateBinaryNameFromPath("openvpn-gui.exe"))
	assert.Equal(t, "Portmaster Core v0-1-2", GenerateBinaryNameFromPath("portmaster-core_v0-1-2.exe"))
	assert.Equal(t, "Win Store App", GenerateBinaryNameFromPath("WinStore.App.exe"))
	assert.Equal(t, "Test Script", GenerateBinaryNameFromPath(".test-script"))
	assert.Equal(t, "Browser Broker", GenerateBinaryNameFromPath("browser_broker.exe"))
	assert.Equal(t, "Virtual Box VM", GenerateBinaryNameFromPath("VirtualBoxVM"))
	assert.Equal(t, "Io Elementary Appcenter", GenerateBinaryNameFromPath("io.elementary.appcenter"))
	assert.Equal(t, "Microsoft Windows Store", GenerateBinaryNameFromPath("Microsoft.WindowsStore"))
}

func TestCleanFileDescription(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Product Name", cleanFileDescription("Product Name"))
	assert.Equal(t, "Product Name", cleanFileDescription("Product Name. Does this and that."))
	assert.Equal(t, "Product Name", cleanFileDescription("Product Name - Does this and that."))
	assert.Equal(t, "Product Name", cleanFileDescription("Product Name / Does this and that."))
	assert.Equal(t, "Product Name", cleanFileDescription("Product Name :: Does this and that."))
	assert.Equal(t, "/ Product Name", cleanFileDescription("/ Product Name"))
	assert.Equal(t, "Product", cleanFileDescription("Product / Name"))
	assert.Equal(t, "Software 2", cleanFileDescription("Software 2"))
	assert.Equal(t, "Launcher for Software 2", cleanFileDescription("Launcher for 'Software 2'"))
	assert.Equal(t, "", cleanFileDescription(". / Name"))
	assert.Equal(t, "", cleanFileDescription(". "))
	assert.Equal(t, "", cleanFileDescription("."))
	assert.Equal(t, "N/A", cleanFileDescription("N/A"))

	assert.Equal(t,
		"Product Name a Does this and that.",
		cleanFileDescription("Product Name a Does this and that."),
	)
}
