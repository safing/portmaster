package access

import (
	"os"
	"testing"
)

var (
	testUsername = os.Getenv("SPN_TEST_USERNAME")
	testPassword = os.Getenv("SPN_TEST_PASSWORD")
)

func TestClient(t *testing.T) {
	// Skip test in CI.
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	if testUsername == "" || testPassword == "" {
		t.Fatal("test username or password not configured")
	}

	loginAndRefresh(t, true, 5)
	clearUserCaches()
	loginAndRefresh(t, false, 1)

	err := Logout(false, false)
	if err != nil {
		t.Fatalf("failed to log out: %s", err)
	}
	t.Logf("logged out")

	loginAndRefresh(t, true, 1)

	err = Logout(false, true)
	if err != nil {
		t.Fatalf("failed to log out: %s", err)
	}
	t.Logf("logged out with purge")

	loginAndRefresh(t, true, 1)
}

func loginAndRefresh(t *testing.T, doLogin bool, refreshTimes int) {
	t.Helper()

	if doLogin {
		_, _, err := Login(testUsername, testPassword)
		if err != nil {
			t.Fatalf("login failed: %s", err)
		}
		user, err := GetUser()
		if err != nil {
			t.Fatalf("failed to get user: %s", err)
		}
		t.Logf("user (from login): %+v", user.User)
		t.Logf("device (from login): %+v", user.User.Device)
		authToken, err := GetAuthToken()
		if err != nil {
			t.Fatalf("failed to get auth token: %s", err)
		}
		t.Logf("auth token: %+v", authToken.Token)
	}

	for range refreshTimes {
		user, _, err := UpdateUser()
		if err != nil {
			t.Fatalf("getting profile failed: %s", err)
		}
		t.Logf("user (from refresh): %+v", user.User)

		authToken, err := GetAuthToken()
		if err != nil {
			t.Fatalf("failed to get auth token: %s", err)
		}
		t.Logf("auth token: %+v", authToken.Token)
	}
}
