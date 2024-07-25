package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testToken = new(AuthToken)

func testAuthenticator(r *http.Request, s *http.Server) (*AuthToken, error) {
	switch {
	case testToken.Read == -127 || testToken.Write == -127:
		return nil, errors.New("test error")
	case testToken.Read == -128 || testToken.Write == -128:
		return nil, fmt.Errorf("%wdenied", ErrAPIAccessDeniedMessage)
	default:
		return testToken, nil
	}
}

type testAuthHandler struct {
	Read  Permission
	Write Permission
}

func (ah *testAuthHandler) ReadPermission(r *http.Request) Permission {
	return ah.Read
}

func (ah *testAuthHandler) WritePermission(r *http.Request) Permission {
	return ah.Write
}

func (ah *testAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if request is as expected.
	ar := GetAPIRequest(r)
	switch {
	case ar == nil:
		http.Error(w, "ar == nil", http.StatusInternalServerError)
	case ar.AuthToken == nil:
		http.Error(w, "ar.AuthToken == nil", http.StatusInternalServerError)
	default:
		http.Error(w, "auth success", http.StatusOK)
	}
}

func makeAuthTestPath(reading bool, p Permission) string {
	if reading {
		return fmt.Sprintf("/test/auth/read/%s", p)
	}
	return fmt.Sprintf("/test/auth/write/%s", p)
}

func TestPermissions(t *testing.T) {
	t.Parallel()

	testHandler := &mainHandler{
		mux: mainMux,
	}

	// Define permissions that need testing.
	permissionsToTest := []Permission{
		NotSupported,
		PermitAnyone,
		PermitUser,
		PermitAdmin,
		PermitSelf,
		Dynamic,
		NotFound,
		100,  // Test a too high value.
		-100, // Test a too low value.
		-127, // Simulate authenticator failure.
		-128, // Simulate authentication denied message.
	}

	// Register test handlers.
	for _, p := range permissionsToTest {
		RegisterHandler(makeAuthTestPath(true, p), &testAuthHandler{Read: p})
		RegisterHandler(makeAuthTestPath(false, p), &testAuthHandler{Write: p})
	}

	// Test all the combinations.
	for _, requestPerm := range permissionsToTest {
		for _, handlerPerm := range permissionsToTest {
			for _, method := range []string{
				http.MethodGet,
				http.MethodHead,
				http.MethodPost,
				http.MethodPut,
				http.MethodDelete,
			} {

				// Set request permission for test requests.
				_, reading, _ := getEffectiveMethod(&http.Request{Method: method})
				if reading {
					testToken.Read = requestPerm
					testToken.Write = NotSupported
				} else {
					testToken.Read = NotSupported
					testToken.Write = requestPerm
				}

				// Evaluate expected result.
				var expectSuccess bool
				switch {
				case handlerPerm == PermitAnyone:
					// This is fast-tracked. There are not additional checks.
					expectSuccess = true
				case handlerPerm == Dynamic:
					// This is turned into PermitAnyone in the authenticator.
					// But authentication is still processed and the result still gets
					// sanity checked!
					if requestPerm >= PermitAnyone &&
						requestPerm <= PermitSelf {
						expectSuccess = true
					}
					// Another special case is when the handler requires permission to be
					// processed but the authenticator fails to authenticate the request.
					// In this case, a fallback token with PermitAnyone is used.
					if requestPerm == -128 {
						// -128 is used to simulate a permission denied message.
						expectSuccess = true
					}
				case handlerPerm <= NotSupported:
					// Invalid handler permission.
				case handlerPerm > PermitSelf:
					// Invalid handler permission.
				case requestPerm <= NotSupported:
					// Invalid request permission.
				case requestPerm > PermitSelf:
					// Invalid request permission.
				case requestPerm < handlerPerm:
					// Valid, but insufficient request permission.
				default:
					expectSuccess = true
				}

				if expectSuccess {
					// Test for success.
					if !assert.HTTPBodyContains(
						t,
						testHandler.ServeHTTP,
						method,
						makeAuthTestPath(reading, handlerPerm),
						nil,
						"auth success",
					) {
						t.Errorf(
							"%s with %s (%d) to handler %s (%d)",
							method,
							requestPerm, requestPerm,
							handlerPerm, handlerPerm,
						)
					}
				} else {
					// Test for error.
					if !assert.HTTPError(t,
						testHandler.ServeHTTP,
						method,
						makeAuthTestPath(reading, handlerPerm),
						nil,
					) {
						t.Errorf(
							"%s with %s (%d) to handler %s (%d)",
							method,
							requestPerm, requestPerm,
							handlerPerm, handlerPerm,
						)
					}
				}
			}
		}
	}
}

func TestPermissionDefinitions(t *testing.T) {
	t.Parallel()

	if NotSupported != 0 {
		t.Fatalf("NotSupported must be zero, was %v", NotSupported)
	}
}
