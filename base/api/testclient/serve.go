package testclient

import (
	"net/http"

	"github.com/safing/portmaster/base/api"
)

func init() {
	api.RegisterHandler("/test/", http.StripPrefix("/test/", http.FileServer(http.Dir("./api/testclient/root/"))))
}
