package profile

// DEACTIVATED

// import (
// 	"testing"
// )
//
// func testGetNewPath(t *testing.T, f *Framework, command, cwd, expect string) {
// 	newPath, err := f.GetNewPath(command, cwd)
// 	if err != nil {
// 		t.Errorf("GetNewPath failed: %s", err)
// 	}
// 	if newPath != expect {
// 		t.Errorf("GetNewPath return unexpected result: got %s, expected %s", newPath, expect)
// 	}
// }
//
// func TestFramework(t *testing.T) {
// 	f1 := &Framework{
// 		Find:  "([^ ]+)$",
// 		Build: "{CWD}/{1}",
// 	}
// 	testGetNewPath(t, f1, "/usr/bin/python bash", "/bin", "/bin/bash")
// 	f2 := &Framework{
// 		Find:  "([^ ]+)$",
// 		Build: "{1}|{CWD}/{1}",
// 	}
// 	testGetNewPath(t, f2, "/usr/bin/python /bin/bash", "/tmp", "/bin/bash")
// }
