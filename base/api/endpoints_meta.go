package api

import (
	"encoding/json"
	"errors"
	"net/http"
)

func registerMetaEndpoints() error {
	if err := RegisterEndpoint(Endpoint{
		Path:        "endpoints",
		Read:        PermitAnyone,
		MimeType:    MimeTypeJSON,
		DataFunc:    listEndpoints,
		Name:        "Export API Endpoints",
		Description: "Returns a list of all registered endpoints and their metadata.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "auth/permissions",
		Read:        Dynamic,
		StructFunc:  permissions,
		Name:        "View Current Permissions",
		Description: "Returns the current permissions assigned to the request.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "auth/bearer",
		Read:        Dynamic,
		HandlerFunc: authBearer,
		Name:        "Request HTTP Bearer Auth",
		Description: "Returns an HTTP Bearer Auth request, if not authenticated.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "auth/basic",
		Read:        Dynamic,
		HandlerFunc: authBasic,
		Name:        "Request HTTP Basic Auth",
		Description: "Returns an HTTP Basic Auth request, if not authenticated.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "auth/reset",
		Read:        PermitAnyone,
		HandlerFunc: authReset,
		Name:        "Reset Authenticated Session",
		Description: "Resets authentication status internally and in the browser.",
	}); err != nil {
		return err
	}

	return nil
}

func listEndpoints(ar *Request) (data []byte, err error) {
	data, err = json.Marshal(ExportEndpoints())
	return
}

func permissions(ar *Request) (i interface{}, err error) {
	if ar.AuthToken == nil {
		return nil, errors.New("authentication token missing")
	}

	return struct {
		Read      Permission
		Write     Permission
		ReadRole  string
		WriteRole string
	}{
		Read:      ar.AuthToken.Read,
		Write:     ar.AuthToken.Write,
		ReadRole:  ar.AuthToken.Read.Role(),
		WriteRole: ar.AuthToken.Write.Role(),
	}, nil
}

func authBearer(w http.ResponseWriter, r *http.Request) {
	// Check if authenticated by checking read permission.
	ar := GetAPIRequest(r)
	if ar.AuthToken.Read != PermitAnyone {
		TextResponse(w, r, "Authenticated.")
		return
	}

	// Respond with desired authentication header.
	w.Header().Set(
		"WWW-Authenticate",
		`Bearer realm="Portmaster API" domain="/"`,
	)
	http.Error(w, "Authorization required.", http.StatusUnauthorized)
}

func authBasic(w http.ResponseWriter, r *http.Request) {
	// Check if authenticated by checking read permission.
	ar := GetAPIRequest(r)
	if ar.AuthToken.Read != PermitAnyone {
		TextResponse(w, r, "Authenticated.")
		return
	}

	// Respond with desired authentication header.
	w.Header().Set(
		"WWW-Authenticate",
		`Basic realm="Portmaster API" domain="/"`,
	)
	http.Error(w, "Authorization required.", http.StatusUnauthorized)
}

func authReset(w http.ResponseWriter, r *http.Request) {
	// Get session cookie from request and delete session if exists.
	c, err := r.Cookie(sessionCookieName)
	if err == nil {
		deleteSession(c.Value)
	}

	// Delete session and cookie.
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookieName,
		MaxAge: -1, // MaxAge<0 means delete cookie now, equivalently 'Max-Age: 0'
	})

	// Request client to also reset all data.
	w.Header().Set("Clear-Site-Data", "*")

	// Set HTTP Auth Realm without requesting authorization.
	w.Header().Set("WWW-Authenticate", `None realm="Portmaster API"`)

	// Reply with 401 Unauthorized in order to clear HTTP Basic Auth data.
	http.Error(w, "Session deleted.", http.StatusUnauthorized)
}
