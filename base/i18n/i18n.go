// Package i18n provides internationalization support for the Portmaster.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/log"
)

//go:embed translations/*.json
var translationsFS embed.FS

// Translation represents a single translation entry.
type Translation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"`
}

// Translations holds all translations for a language.
type Translations struct {
	Language string                 `json:"language"`
	Config   map[string]Translation `json:"config"`
	UI       map[string]string      `json:"ui"`
}

var (
	currentLang    = "en"
	translations   = make(map[string]*Translations)
	translationsMu sync.RWMutex
	initialized    bool
)

// SupportedLanguages returns list of supported languages.
var SupportedLanguages = []string{"en", "ru"}

// Init initializes the i18n system.
func Init() error {
	translationsMu.Lock()
	defer translationsMu.Unlock()

	if initialized {
		return nil
	}

	for _, lang := range SupportedLanguages {
		data, err := translationsFS.ReadFile(fmt.Sprintf("translations/%s.json", lang))
		if err != nil {
			log.Warningf("i18n: failed to load translations for %s: %s", lang, err)
			continue
		}

		var t Translations
		if err := json.Unmarshal(data, &t); err != nil {
			log.Warningf("i18n: failed to parse translations for %s: %s", lang, err)
			continue
		}

		t.Language = lang
		translations[lang] = &t
		log.Infof("i18n: loaded %d config translations for %s", len(t.Config), lang)
	}

	initialized = true
	return nil
}

// SetLanguage sets the current language.
func SetLanguage(lang string) error {
	translationsMu.Lock()
	defer translationsMu.Unlock()

	if _, ok := translations[lang]; !ok {
		return fmt.Errorf("i18n: language %s not supported", lang)
	}

	currentLang = lang
	log.Infof("i18n: language set to %s", lang)
	return nil
}

// GetLanguage returns the current language.
func GetLanguage() string {
	translationsMu.RLock()
	defer translationsMu.RUnlock()
	return currentLang
}

// T returns translated string for UI key.
func T(key string) string {
	translationsMu.RLock()
	defer translationsMu.RUnlock()

	if t, ok := translations[currentLang]; ok {
		if val, ok := t.UI[key]; ok {
			return val
		}
	}

	// Fallback to English
	if t, ok := translations["en"]; ok {
		if val, ok := t.UI[key]; ok {
			return val
		}
	}

	return key
}

// GetConfigTranslation returns translation for a config key.
func GetConfigTranslation(key string) *Translation {
	translationsMu.RLock()
	defer translationsMu.RUnlock()

	if t, ok := translations[currentLang]; ok {
		if val, ok := t.Config[key]; ok {
			return &val
		}
	}

	// Fallback to English
	if t, ok := translations["en"]; ok {
		if val, ok := t.Config[key]; ok {
			return &val
		}
	}

	return nil
}

// GetConfigName returns translated name for a config key.
func GetConfigName(key, fallback string) string {
	if t := GetConfigTranslation(key); t != nil && t.Name != "" {
		return t.Name
	}
	return fallback
}

// GetConfigDescription returns translated description for a config key.
func GetConfigDescription(key, fallback string) string {
	if t := GetConfigTranslation(key); t != nil && t.Description != "" {
		return t.Description
	}
	return fallback
}

// GetConfigCategory returns translated category for a config key.
func GetConfigCategory(key, fallback string) string {
	if t := GetConfigTranslation(key); t != nil && t.Category != "" {
		return t.Category
	}
	return fallback
}
