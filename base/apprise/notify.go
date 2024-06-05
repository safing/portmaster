package apprise

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/safing/portmaster/base/utils"
)

// Notifier sends messsages to an Apprise API.
type Notifier struct {
	// URL defines the Apprise API endpoint.
	URL string

	// DefaultType defines the default message type.
	DefaultType MsgType

	// DefaultTag defines the default message tag.
	DefaultTag string

	// DefaultFormat defines the default message format.
	DefaultFormat MsgFormat

	// AllowUntagged defines if untagged messages are allowed,
	// which are sent to all configured apprise endpoints.
	AllowUntagged bool

	client     *http.Client
	clientLock sync.Mutex
}

// Message represents the message to be sent to the Apprise API.
type Message struct {
	// Title is an optional title to go along with the body.
	Title string `json:"title,omitempty"`

	// Body is the main message content. This is the only required field.
	Body string `json:"body"`

	// Type defines the message type you want to send as.
	// The valid options are info, success, warning, and failure.
	// If no type is specified then info is the default value used.
	Type MsgType `json:"type,omitempty"`

	// Tag is used to notify only those tagged accordingly.
	// Use a comma (,) to OR your tags and a space ( ) to AND them.
	Tag string `json:"tag,omitempty"`

	// Format optionally identifies the text format of the data you're feeding Apprise.
	// The valid options are text, markdown, html.
	// The default value if nothing is specified is text.
	Format MsgFormat `json:"format,omitempty"`
}

// MsgType defines the message type.
type MsgType string

// Message Types.
const (
	TypeInfo    MsgType = "info"
	TypeSuccess MsgType = "success"
	TypeWarning MsgType = "warning"
	TypeFailure MsgType = "failure"
)

// MsgFormat defines the message format.
type MsgFormat string

// Message Formats.
const (
	FormatText     MsgFormat = "text"
	FormatMarkdown MsgFormat = "markdown"
	FormatHTML     MsgFormat = "html"
)

type errorResponse struct {
	Error string `json:"error"`
}

// Send sends a message to the Apprise API.
func (n *Notifier) Send(ctx context.Context, m *Message) error {
	// Check if the message has a body.
	if m.Body == "" {
		return errors.New("the message must have a body")
	}

	// Apply notifier defaults.
	n.applyDefaults(m)

	// Check if the message is tagged.
	if m.Tag == "" && !n.AllowUntagged {
		return errors.New("the message must have a tag")
	}

	// Marshal the message to JSON.
	payload, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Create request.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	// Send message to API.
	resp, err := n.getClient().Do(request)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck,gosec
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusAccepted:
		return nil
	default:
		// Try to tease body contents.
		if body, err := io.ReadAll(resp.Body); err == nil && len(body) > 0 {
			// Try to parse json response.
			errorResponse := &errorResponse{}
			if err := json.Unmarshal(body, errorResponse); err == nil && errorResponse.Error != "" {
				return fmt.Errorf("failed to send message: apprise returned %q with an error message: %s", resp.Status, errorResponse.Error)
			}
			return fmt.Errorf("failed to send message: %s (body teaser: %s)", resp.Status, utils.SafeFirst16Bytes(body))
		}
		return fmt.Errorf("failed to send message: %s", resp.Status)
	}
}

func (n *Notifier) applyDefaults(m *Message) {
	if m.Type == "" {
		m.Type = n.DefaultType
	}
	if m.Tag == "" {
		m.Tag = n.DefaultTag
	}
	if m.Format == "" {
		m.Format = n.DefaultFormat
	}
}

// SetClient sets a custom http client for accessing the Apprise API.
func (n *Notifier) SetClient(client *http.Client) {
	n.clientLock.Lock()
	defer n.clientLock.Unlock()

	n.client = client
}

func (n *Notifier) getClient() *http.Client {
	n.clientLock.Lock()
	defer n.clientLock.Unlock()

	// Create client if needed.
	if n.client == nil {
		n.client = &http.Client{}
	}

	return n.client
}
