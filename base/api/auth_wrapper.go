package api

import "net/http"

// WrapInAuthHandler wraps a simple http.HandlerFunc into a handler that
// exposes the required API permissions for this handler.
func WrapInAuthHandler(fn http.HandlerFunc, read, write Permission) http.Handler {
	return &wrappedAuthenticatedHandler{
		HandlerFunc: fn,
		read:        read,
		write:       write,
	}
}

type wrappedAuthenticatedHandler struct {
	http.HandlerFunc

	read  Permission
	write Permission
}

// ReadPermission returns the read permission for the handler.
func (wah *wrappedAuthenticatedHandler) ReadPermission(r *http.Request) Permission {
	return wah.read
}

// WritePermission returns the write permission for the handler.
func (wah *wrappedAuthenticatedHandler) WritePermission(r *http.Request) Permission {
	return wah.write
}
