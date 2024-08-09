package main

import (
	"bytes"
	"crypto/tls"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/safing/portmaster/base/apprise"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/mgr"
)

// Apprise is the apprise notification module.
type Apprise struct {
	mgr *mgr.Manager

	instance instance
}

// Manager returns the module manager.
func (a *Apprise) Manager() *mgr.Manager {
	return a.mgr
}

// Start starts the module.
func (a *Apprise) Start() error {
	return startApprise()
}

// Stop stops the module.
func (a *Apprise) Stop() error {
	return nil
}

var (
	appriseModule     *Apprise
	appriseShimLoaded atomic.Bool
	appriseNotifier   *apprise.Notifier

	appriseURL        string
	appriseTag        string
	appriseClientCert string
	appriseClientKey  string
	appriseGreet      bool
)

func init() {
	// appriseModule = modules.Register("apprise", nil, startApprise, nil)

	flag.StringVar(&appriseURL, "apprise-url", "", "set the apprise URL to enable notifications via apprise")
	flag.StringVar(&appriseTag, "apprise-tag", "", "set the apprise tag(s) according to their docs")
	flag.StringVar(&appriseClientCert, "apprise-client-cert", "", "set the apprise client certificate")
	flag.StringVar(&appriseClientKey, "apprise-client-key", "", "set the apprise client key")
	flag.BoolVar(&appriseGreet, "apprise-greet", false, "send a greeting message to apprise on start")
}

func startApprise() error {
	// Check if apprise should be configured.
	if appriseURL == "" {
		return nil
	}
	// Check if there is a tag.
	if appriseTag == "" {
		return errors.New("an apprise tag is required")
	}

	// Create notifier.
	appriseNotifier = &apprise.Notifier{
		URL:           appriseURL,
		DefaultType:   apprise.TypeInfo,
		DefaultTag:    appriseTag,
		DefaultFormat: apprise.FormatMarkdown,
		AllowUntagged: false,
	}

	if appriseClientCert != "" || appriseClientKey != "" {
		// Load client cert from disk.
		cert, err := tls.LoadX509KeyPair(appriseClientCert, appriseClientKey)
		if err != nil {
			return fmt.Errorf("failed to load client cert/key: %w", err)
		}

		// Set client cert in http client.
		appriseNotifier.SetClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion:   tls.VersionTLS12,
					Certificates: []tls.Certificate{cert},
				},
			},
			Timeout: 10 * time.Second,
		})
	}

	if appriseGreet {
		err := appriseNotifier.Send(appriseModule.mgr.Ctx(), &apprise.Message{
			Title: "👋 Observation Hub Reporting In",
			Body:  "I am the Observation Hub. I am connected to the SPN and watch out for it. I will report notable changes to the network here.",
		})
		if err != nil {
			log.Warningf("apprise: failed to send test message: %s", err)
		} else {
			log.Info("apprise: sent greeting message")
		}
	}

	return nil
}

func reportToApprise(change *observedChange) (errs error) {
	// Check if configured.
	if appriseNotifier == nil {
		return nil
	}

handleTag:
	for _, tag := range strings.Split(appriseNotifier.DefaultTag, ",") {
		// Check if we are shutting down.
		if appriseModule.mgr.IsDone() {
			return nil
		}

		// Render notification based on tag / destination.
		buf := &bytes.Buffer{}
		switch {
		case strings.HasPrefix(tag, "matrix-"):
			if err := templates.ExecuteTemplate(buf, "matrix-notification", change); err != nil {
				return fmt.Errorf("failed to render notification: %w", err)
			}

		case strings.HasPrefix(tag, "discord-"):
			if err := templates.ExecuteTemplate(buf, "discord-notification", change); err != nil {
				return fmt.Errorf("failed to render notification: %w", err)
			}

		default:
			// Use matrix notification template as default for now.
			if err := templates.ExecuteTemplate(buf, "matrix-notification", change); err != nil {
				return fmt.Errorf("failed to render notification: %w", err)
			}
		}

		// Send notification to apprise.
		var err error
		for range 3 {
			// Try three times.
			err = appriseNotifier.Send(appriseModule.mgr.Ctx(), &apprise.Message{
				Body: buf.String(),
				Tag:  tag,
			})
			if err == nil {
				continue handleTag
			}
			// Wait for 5 seconds, then try again.
			time.Sleep(5 * time.Second)
		}
		// Add error to errors.
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("| failed to send: %w", err))
		}
	}

	return errs
}

// var (
// 	entityTemplate = template.Must(template.New("entity").Parse(
// 		`Entity: {{ . }}
// {{ .IP }} [{{ .ASN }} - {{ .ASOrg }}]
// `,
// 	))

// 	// {{ with .GetCountryInfo -}}
// 	// {{ .Name }} ({{ .Code }})
// 	// {{- end }}

// 	matrixTemplate = template.Must(template.New("matrix observer notification").Parse(
// 		`{{ .Title }}
// {{ if .Summary }}
// Details:
// {{ .Summary }}

// Note: Changes were registered at {{ .UpdateTime }} and were possibly merged.
// {{ end }}

// {{ template "entity" .UpdatedPin.EntityV4 }}

// Hub Info:
// Test: {{ .UpdatedPin.EntityV4 }}
// {{ template "entity" .UpdatedPin.EntityV4 }}
// {{ template "entity" .UpdatedPin.EntityV6 }}
// `,
// 	))

// 	discordTemplate = template.Must(template.New("discord observer notification").Parse(
// 		``,
// 	))

// 	defaultTemplate = template.Must(template.New("default observer notification").Parse(
// 		``,
// 	))
// )

var (
	//go:embed notifications.tmpl
	templateFile string
	templates    = template.Must(template.New("notifications").Funcs(
		template.FuncMap{
			"joinStrings":    joinStrings,
			"textBlock":      textBlock,
			"getCountryInfo": getCountryInfo,
		},
	).Parse(templateFile))
)

func joinStrings(slice []string, sep string) string {
	return strings.Join(slice, sep)
}

func textBlock(block, addPrefix, addSuffix string) string {
	// Trim whitespaces.
	block = strings.TrimSpace(block)

	// Prepend and append string for every line.
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = addPrefix + line + addSuffix
	}

	// Return as block.
	return strings.Join(lines, "\n")
}

func getCountryInfo(code string) geoip.CountryInfo {
	// Get the country info directly instead of via the entity location,
	// so it also works in test without the geoip module.
	return geoip.GetCountryInfo(code)
}

// func init() {
// 	templates = template.Must(template.New(templateFile).Parse(templateFile))

// 	nt, err := templates.New("entity").Parse(
// 		`Entity: {{ . }}
// {{ .IP }} [{{ .ASN }} - {{ .ASOrg }}]
// `,
// 	)
// 	if err != nil {
// 		panic(err)
// 	}
// 	templates.AddParseTree(nt.Tree)

// 	if _, err := templates.New("matrix-notification").Parse(
// 		`{{ .Title }}
// {{ if .Summary }}
// Details:
// {{ .Summary }}

// Note: Changes were registered at {{ .UpdateTime }} and were possibly merged.
// {{ end }}

// {{ template "entity" .UpdatedPin.EntityV4 }}

// Hub Info:
// Test: {{ .UpdatedPin.EntityV4 }}
// {{ template "entity" .UpdatedPin.EntityV4 }}
// {{ template "entity" .UpdatedPin.EntityV6 }}
// `,
// 	); err != nil {
// 		panic(err)
// 	}
// }

// NewApprise returns a new Apprise module.
func NewApprise(instance instance) (*Observer, error) {
	if !appriseShimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("apprise")
	appriseModule = &Apprise{
		mgr:      m,
		instance: instance,
	}

	return observerModule, nil
}
