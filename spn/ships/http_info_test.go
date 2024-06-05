package ships

import (
	"html/template"
	"testing"

	"github.com/safing/portmaster/base/config"
)

func TestInfoPageTemplate(t *testing.T) {
	t.Parallel()

	infoPageTemplate = template.Must(template.New("info-page").Parse(infoPageData))
	pageInputName = config.Concurrent.GetAsString("spn/publicHub/name", "node-name")
	pageInputGroup = config.Concurrent.GetAsString("spn/publicHub/group", "node-group")
	pageInputContactAddress = config.Concurrent.GetAsString("spn/publicHub/contactAddress", "john@doe.com")
	pageInputContactService = config.Concurrent.GetAsString("spn/publicHub/contactService", "email")

	pageData, err := renderInfoPage()
	if err != nil {
		t.Fatal(err)
	}

	_ = pageData
	// t.Log(string(pageData))
}
