package firewall

import "testing"

func TestIsEmbeddedWebViewProcess(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		`C:\Program Files (x86)\Microsoft\EdgeWebView\Application\157.0.4341.60\msedgewebview2.exe`: true,
		`C:\Program Files (x86)\Microsoft\EdgeWebView\Application\157.0.4341.60\MSEDGEWEBVIEW2.EXE`: true,
		"/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1/WebKitNetworkProcess":                             true,
		"/usr/libexec/webkit2gtk-4.1/WebKitWebProcess":                                              true,
		`C:\Program Files\Mozilla Firefox\firefox.exe`:                                              false,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`:                              false,
		"/usr/bin/firefox":               false,
		"/usr/bin/msedgewebview2.exe.sh": false,
		"":                               false,
	} {
		if got := isEmbeddedWebViewProcess(path); got != want {
			t.Errorf("isEmbeddedWebViewProcess(%q) = %v, want %v", path, got, want)
		}
	}
}
