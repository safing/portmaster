package ships

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
)

var (
	//go:embed http_info_page.html.tmpl
	infoPageData string

	infoPageTemplate *template.Template

	// DisplayHubID holds the Hub ID for displaying it on the info page.
	DisplayHubID string
)

type infoPageInput struct {
	Version        string
	Info           *info.Info
	ID             string
	Name           string
	Group          string
	ContactAddress string
	ContactService string
}

var (
	pageInputName           config.StringOption
	pageInputGroup          config.StringOption
	pageInputContactAddress config.StringOption
	pageInputContactService config.StringOption
)

func initPageInput() {
	infoPageTemplate = template.Must(template.New("info-page").Parse(infoPageData))

	pageInputName = config.Concurrent.GetAsString("spn/publicHub/name", "")
	pageInputGroup = config.Concurrent.GetAsString("spn/publicHub/group", "")
	pageInputContactAddress = config.Concurrent.GetAsString("spn/publicHub/contactAddress", "")
	pageInputContactService = config.Concurrent.GetAsString("spn/publicHub/contactService", "")
}

// ServeInfoPage serves the info page for the given request.
func ServeInfoPage(w http.ResponseWriter, r *http.Request) {
	pageData, err := renderInfoPage()
	if err != nil {
		log.Warningf("ships: failed to render SPN info page: %s", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	_, err = w.Write(pageData)
	if err != nil {
		log.Warningf("ships: failed to write info page: %s", err)
	}
}

func renderInfoPage() ([]byte, error) {
	input := &infoPageInput{
		Version:        info.Version(),
		Info:           info.GetInfo(),
		ID:             DisplayHubID,
		Name:           pageInputName(),
		Group:          pageInputGroup(),
		ContactAddress: pageInputContactAddress(),
		ContactService: pageInputContactService(),
	}

	buf := &bytes.Buffer{}
	err := infoPageTemplate.ExecuteTemplate(buf, "info-page", input)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
