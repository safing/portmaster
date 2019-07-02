package profile

import "testing"

func testPathID(t *testing.T, execPath, identifierPath string) {
	result := GetPathIdentifier(execPath)
	if result != identifierPath {
		t.Errorf("unexpected identifier path for %s: got %s, expected %s", execPath, result, identifierPath)
	}
}

func TestGetPathIdentifier(t *testing.T) {
	testPathID(t, "/bin/bash", "bin/bash")
	testPathID(t, "/home/user/bin/bash", "bin/bash")
	testPathID(t, "/home/user/project/main", "project/main")
	testPathID(t, "/root/project/main", "project/main")
	testPathID(t, "/tmp/a/b/c/d/install.sh", "c/d/install.sh")
	testPathID(t, "/lib/systemd/systemd-udevd", "lib/systemd/systemd-udevd")
	testPathID(t, "/bundle/ruby/2.4.0/bin/passenger", "bin/passenger")
	testPathID(t, "/usr/sbin/cron", "sbin/cron")
	testPathID(t, "/usr/local/bin/python", "bin/python")
}
