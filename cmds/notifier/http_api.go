package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"github.com/safing/portbase/log"
)

const (
	apiBaseURL          = "http://127.0.0.1:817/api/v1/"
	apiShutdownEndpoint = "core/shutdown"
)

var (
	httpApiClient *http.Client
)

func init() {
	// Make cookie jar.
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Warningf("http-api: failed to create cookie jar: %s", err)
		jar = nil
	}

	// Create client.
	httpApiClient = &http.Client{
		Jar:     jar,
		Timeout: 3 * time.Second,
	}
}

func httpApiAction(endpoint string) (response string, err error) {
	// Make action request.
	resp, err := httpApiClient.Post(apiBaseURL+endpoint, "", nil)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	// Read the response body.
	defer resp.Body.Close()
	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read data: %w", err)
	}
	response = strings.TrimSpace(string(respData))

	// Check if the request was successful on the server.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return response, fmt.Errorf("server failed with %s: %s", resp.Status, response)
	}

	return response, nil
}

// TriggerShutdown triggers a shutdown via the APi.
func TriggerShutdown() error {
	_, err := httpApiAction(apiShutdownEndpoint)
	return err
}
