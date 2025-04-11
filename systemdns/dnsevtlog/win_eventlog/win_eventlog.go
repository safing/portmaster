// The MIT License (MIT)

// Copyright (c) 2015-2020 InfluxData Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//go:build windows
// +build windows

// Package win_eventlog Input plugin to collect Windows Event Log messages
//
//revive:disable-next-line:var-naming
package win_eventlog

import (
	"bytes"
	"encoding/xml"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// WinEventLog config
type WinEventLog struct {
	Locale                 uint32   `yaml:"locale"`
	EventlogName           string   `yaml:"eventlog_name"`
	Query                  string   `yaml:"xpath_query"`
	ProcessUserData        bool     `yaml:"process_userdata"`
	ProcessEventData       bool     `yaml:"process_eventdata"`
	Separator              string   `yaml:"separator"`
	OnlyFirstLineOfMessage bool     `yaml:"only_first_line_of_message"`
	TimeStampFromEvent     bool     `yaml:"timestamp_from_event"`
	EventTags              []string `yaml:"event_tags"`
	EventFields            []string `yaml:"event_fields"`
	ExcludeFields          []string `yaml:"exclude_fields"`
	ExcludeEmpty           []string `yaml:"exclude_empty"`

	subscription EvtHandle
	buf          []byte
}

var bufferSize = 1 << 14

var description = "Input plugin to collect Windows Event Log messages"

// Description for win_eventlog
func (w *WinEventLog) Description() string {
	return description
}

func (w *WinEventLog) shouldExclude(field string) (should bool) {
	for _, excludePattern := range w.ExcludeFields {
		// Check if field name matches excluded list
		if matched, _ := filepath.Match(excludePattern, field); matched {
			return true
		}
	}
	return false
}

func (w *WinEventLog) shouldProcessField(field string) (should bool, list string) {
	for _, pattern := range w.EventTags {
		if matched, _ := filepath.Match(pattern, field); matched {
			// Tags are not excluded
			return true, "tags"
		}
	}

	for _, pattern := range w.EventFields {
		if matched, _ := filepath.Match(pattern, field); matched {
			if w.shouldExclude(field) {
				return false, "excluded"
			}
			return true, "fields"
		}
	}
	return false, "excluded"
}

func (w *WinEventLog) shouldExcludeEmptyField(field string, fieldType string, fieldValue interface{}) (should bool) {
	for _, pattern := range w.ExcludeEmpty {
		if matched, _ := filepath.Match(pattern, field); matched {
			switch fieldType {
			case "string":
				return len(fieldValue.(string)) < 1
			case "int":
				return fieldValue.(int) == 0
			case "uint32":
				return fieldValue.(uint32) == 0
			}
		}
	}
	return false
}

func EvtSubscribe(logName, xquery string) (EvtHandle, error) {
	var logNamePtr, xqueryPtr *uint16

	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(sigEvent)

	logNamePtr, err = syscall.UTF16PtrFromString(logName)
	if err != nil {
		return 0, err
	}

	xqueryPtr, err = syscall.UTF16PtrFromString(xquery)
	if err != nil {
		return 0, err
	}

	subsHandle, err := _EvtSubscribe(0, uintptr(sigEvent), logNamePtr, xqueryPtr,
		0, 0, 0, EvtSubscribeStartAtOldestRecord)
	if err != nil {
		return 0, err
	}

	return subsHandle, nil
}

func EvtSubscribeWithBookmark(logName, xquery string, bookMark EvtHandle) (EvtHandle, error) {
	var logNamePtr, xqueryPtr *uint16

	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(sigEvent)

	logNamePtr, err = syscall.UTF16PtrFromString(logName)
	if err != nil {
		return 0, err
	}

	xqueryPtr, err = syscall.UTF16PtrFromString(xquery)
	if err != nil {
		return 0, err
	}

	subsHandle, err := _EvtSubscribe(0, uintptr(sigEvent), logNamePtr, xqueryPtr,
		bookMark, 0, 0, EvtSubscribeStartAfterBookmark)
	if err != nil {
		return 0, err
	}

	return subsHandle, nil
}

func fetchEventHandles(subsHandle EvtHandle) ([]EvtHandle, error) {
	var eventsNumber uint32
	var evtReturned uint32

	eventsNumber = 5

	eventHandles := make([]EvtHandle, eventsNumber)

	err := _EvtNext(subsHandle, eventsNumber, &eventHandles[0], 0, 0, &evtReturned)
	if err != nil {
		if err == ERROR_INVALID_OPERATION && evtReturned == 0 {
			return nil, ERROR_NO_MORE_ITEMS
		}
		return nil, err
	}

	return eventHandles[:evtReturned], nil
}

type EventFetcher struct {
	buf []byte
}

func NewEventFetcher() *EventFetcher {
	return &EventFetcher{}
}

func (w *EventFetcher) FetchEvents(subsHandle EvtHandle, lang uint32) ([]Event, []EvtHandle, error) {
	if w.buf == nil {
		w.buf = make([]byte, bufferSize)
	}
	var events []Event

	eventHandles, err := fetchEventHandles(subsHandle)
	if err != nil {
		return nil, nil, err
	}

	for _, eventHandle := range eventHandles {
		if eventHandle != 0 {
			event, err := w.renderEvent(eventHandle, lang)
			if err == nil {
				events = append(events, event)
			}
		}
	}

	return events, eventHandles, nil
}

func Close(handles []EvtHandle) error {
	for i := 0; i < len(handles); i++ {
		err := _EvtClose(handles[i])
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *EventFetcher) renderEvent(eventHandle EvtHandle, lang uint32) (Event, error) {
	var bufferUsed, propertyCount uint32

	event := Event{}
	err := _EvtRender(0, eventHandle, EvtRenderEventXml, uint32(len(w.buf)), &w.buf[0], &bufferUsed, &propertyCount)
	if err != nil {
		return event, err
	}

	eventXML, err := DecodeUTF16(w.buf[:bufferUsed])
	if err != nil {
		return event, err
	}
	err = xml.Unmarshal([]byte(eventXML), &event)
	if err != nil {
		// We can return event without most text values,
		// that way we will not loose information
		// This can happen when processing Forwarded Events
		return event, nil
	}

	publisherHandle, err := openPublisherMetadata(0, event.Source.Name, lang)
	if err != nil {
		return event, nil
	}
	defer _EvtClose(publisherHandle)

	// Populating text values
	keywords, err := formatEventString(EvtFormatMessageKeyword, eventHandle, publisherHandle)
	if err == nil {
		event.Keywords = keywords
	}
	message, err := formatEventString(EvtFormatMessageEvent, eventHandle, publisherHandle)
	if err == nil {
		event.Message = message
	}
	level, err := formatEventString(EvtFormatMessageLevel, eventHandle, publisherHandle)
	if err == nil {
		event.LevelText = level
	}
	task, err := formatEventString(EvtFormatMessageTask, eventHandle, publisherHandle)
	if err == nil {
		event.TaskText = task
	}
	opcode, err := formatEventString(EvtFormatMessageOpcode, eventHandle, publisherHandle)
	if err == nil {
		event.OpcodeText = opcode
	}
	return event, nil
}

func formatEventString(
	messageFlag EvtFormatMessageFlag,
	eventHandle EvtHandle,
	publisherHandle EvtHandle,
) (string, error) {
	var bufferUsed uint32
	err := _EvtFormatMessage(publisherHandle, eventHandle, 0, 0, 0, messageFlag,
		0, nil, &bufferUsed)
	if err != nil && err != ERROR_INSUFFICIENT_BUFFER {
		return "", err
	}

	bufferUsed *= 2
	buffer := make([]byte, bufferUsed)
	bufferUsed = 0

	err = _EvtFormatMessage(publisherHandle, eventHandle, 0, 0, 0, messageFlag,
		uint32(len(buffer)/2), &buffer[0], &bufferUsed)
	bufferUsed *= 2
	if err != nil {
		return "", err
	}

	result, err := DecodeUTF16(buffer[:bufferUsed])
	if err != nil {
		return "", err
	}

	var out string
	if messageFlag == EvtFormatMessageKeyword {
		// Keywords are returned as array of a zero-terminated strings
		splitZero := func(c rune) bool { return c == '\x00' }
		eventKeywords := strings.FieldsFunc(string(result), splitZero)
		// So convert them to comma-separated string
		out = strings.Join(eventKeywords, ",")
	} else {
		result := bytes.Trim(result, "\x00")
		out = string(result)
	}
	return out, nil
}

// openPublisherMetadata opens a handle to the publisher's metadata. Close must
// be called on returned EvtHandle when finished with the handle.
func openPublisherMetadata(
	session EvtHandle,
	publisherName string,
	lang uint32,
) (EvtHandle, error) {
	p, err := syscall.UTF16PtrFromString(publisherName)
	if err != nil {
		return 0, err
	}

	h, err := _EvtOpenPublisherMetadata(session, p, nil, lang, 0)
	if err != nil {
		return 0, err
	}

	return h, nil
}
