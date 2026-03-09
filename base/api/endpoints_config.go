package api

import (
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/i18n"
)

func registerConfigEndpoints() error {
	if err := RegisterEndpoint(Endpoint{
		Path:        "config/options",
		Read:        PermitAnyone,
		MimeType:    MimeTypeJSON,
		StructFunc:  listConfig,
		Name:        "Export Configuration Options",
		Description: "Returns a list of all registered configuration options and their metadata. This does not include the current active or default settings. Use ?lang=ru for localized names.",
	}); err != nil {
		return err
	}

	return nil
}

// LocalizedOption is an Option with localized Name and Description.
type LocalizedOption struct {
	*config.Option
	Name        string `json:"Name"`
	Description string `json:"Description"`
}

func listConfig(ar *Request) (i interface{}, err error) {
	// Get language from query parameter
	lang := ar.URL.Query().Get("lang")
	if lang == "" {
		lang = i18n.GetLanguage()
	}

	// Get original options
	opts := config.ExportOptions()

	// If English or no translations, return as-is
	if lang == "en" || lang == "" {
		return opts, nil
	}

	// Set language for translation
	i18n.SetLanguage(lang)

	// Create localized copies
	localizedOpts := make([]*LocalizedOption, len(opts))
	for idx, opt := range opts {
		// Get translated name and description with fallback to original
		name := i18n.GetConfigName(opt.Key, opt.Name)
		desc := i18n.GetConfigDescription(opt.Key, opt.Description)

		localizedOpts[idx] = &LocalizedOption{
			Option:      opt,
			Name:        name,
			Description: desc,
		}
	}

	return localizedOpts, nil
}
