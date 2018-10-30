package profile

import (
	"github.com/Safing/portbase/database"
)

// core:profiles/user/12345-1234-125-1234-1235
// core:profiles/special/default
//                      /global
// core:profiles/stamp/12334-1235-1234-5123-1234
// core:profiles/identifier/base64

// Namespaces
const (
	UserNamespace  = "user"
	StampNamespace = "stamp"
)

var (
	profileDB = database.NewInterface(nil)
)
