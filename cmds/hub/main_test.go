package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDirs(t *testing.T) {
	t.Parallel()

	// Directories below the test root.
	const (
		data     = "spn"
		dataLink = "link" // Symlink to data.
		bin      = "spn/bin"
		logs     = "spn/logs"
		outerBin = "bin"
		sibling  = "spn-other"
		parent   = "."
		none     = ""
	)

	// Use real directories, so that symlinks can be resolved.
	root := t.TempDir()
	for _, dir := range []string{bin, logs, outerBin, sibling} {
		err := os.MkdirAll(filepath.Join(root, dir), 0o750)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Creating symlinks needs special rights on some systems.
	linkErr := os.Symlink(filepath.Join(root, data), filepath.Join(root, dataLink))

	// inRoot returns the path below the test root, without cleaning it.
	inRoot := func(dir string) string {
		if dir == none {
			return none
		}
		return root + string(filepath.Separator) + dir
	}

	tests := []struct {
		name      string
		binDir    string
		dataDir   string
		logDir    string
		exeDir    string
		needsLink bool
		ok        bool
	}{
		{name: "separate dirs inside data dir", binDir: bin, dataDir: data, logDir: logs, exeDir: bin, ok: true},
		{name: "bin dir outside of data dir", binDir: outerBin, dataDir: data, logDir: logs, exeDir: outerBin, ok: true},
		{name: "no log dir", binDir: bin, dataDir: data, ok: true},
		{name: "no log dir, program in data dir", binDir: bin, dataDir: data, exeDir: data, ok: true},
		{name: "log dir is parent of data dir", binDir: bin, dataDir: data, logDir: parent, exeDir: bin, ok: true},
		{name: "sibling with common name prefix", binDir: data, dataDir: sibling, ok: true},
		{name: "dirs that do not exist yet", binDir: "new/bin", dataDir: "new", logDir: "new/logs", ok: true},
		{name: "program dir unknown", binDir: bin, dataDir: data, logDir: logs, ok: true},

		{name: "data dir is bin dir", binDir: data, dataDir: data},
		{name: "data dir is bin dir, trailing slash", binDir: data + "/", dataDir: data},
		{name: "data dir is bin dir, via symlink", binDir: dataLink, dataDir: data, needsLink: true},
		{name: "data dir is bin dir, via symlink, not existing yet", binDir: dataLink + "/new", dataDir: data + "/new", needsLink: true},
		{name: "data dir inside of bin dir", binDir: parent, dataDir: data},
		{name: "log dir is data dir", binDir: bin, dataDir: data, logDir: data},
		{name: "log dir is data dir, via symlink", binDir: bin, dataDir: data, logDir: dataLink, needsLink: true},
		{name: "log dir is bin dir", binDir: bin, dataDir: data, logDir: bin},
		{name: "log dir inside of bin dir", binDir: outerBin, dataDir: data, logDir: "bin/logs"},
		{name: "log dir is program dir", binDir: bin, dataDir: data, logDir: sibling, exeDir: sibling},
		{name: "log dir is program dir, above data dir", binDir: bin, dataDir: data, logDir: parent, exeDir: parent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.needsLink && linkErr != nil {
				t.Skipf("cannot create symlink: %s", linkErr)
			}

			err := checkDirs(inRoot(tt.binDir), inRoot(tt.dataDir), inRoot(tt.logDir), inRoot(tt.exeDir))
			if tt.ok && err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if !tt.ok && err == nil {
				t.Error("expected an error")
			}
		})
	}
}
