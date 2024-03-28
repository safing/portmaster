//go:build windows

package wintoast

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/tevino/abool"

	"golang.org/x/sys/windows"
)

// WinNotify holds the DLL handle.
type WinToast struct {
	sync.RWMutex

	dll *windows.DLL

	initialized *abool.AtomicBool

	initialize           *windows.Proc
	isInitialized        *windows.Proc
	createNotification   *windows.Proc
	deleteNotification   *windows.Proc
	addButton            *windows.Proc
	setImage             *windows.Proc
	setSound             *windows.Proc
	showNotification     *windows.Proc
	hideNotification     *windows.Proc
	setActivatedCallback *windows.Proc
	setDismissedCallback *windows.Proc
	setFailedCallback    *windows.Proc
}

func New(dllPath string) (*WinToast, error) {
	if dllPath == "" {
		return nil, fmt.Errorf("winnotifiy: path to dll not specified")
	}

	libraryObject := &WinToast{}
	libraryObject.initialized = abool.New()

	// load dll
	var err error
	libraryObject.dll, err = windows.LoadDLL(dllPath)
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: failed to load notifier dll %w", err)
	}

	// load functions
	libraryObject.initialize, err = libraryObject.dll.FindProc("PortmasterToastInitialize")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastInitialize not found %w", err)
	}

	libraryObject.isInitialized, err = libraryObject.dll.FindProc("PortmasterToastIsInitialized")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastIsInitialized not found %w", err)
	}

	libraryObject.createNotification, err = libraryObject.dll.FindProc("PortmasterToastCreateNotification")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastCreateNotification not found %w", err)
	}

	libraryObject.deleteNotification, err = libraryObject.dll.FindProc("PortmasterToastDeleteNotification")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastDeleteNotification not found %w", err)
	}

	libraryObject.addButton, err = libraryObject.dll.FindProc("PortmasterToastAddButton")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastAddButton not found %w", err)
	}

	libraryObject.setImage, err = libraryObject.dll.FindProc("PortmasterToastSetImage")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastSetImage not found %w", err)
	}

	libraryObject.setSound, err = libraryObject.dll.FindProc("PortmasterToastSetSound")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastSetSound not found %w", err)
	}

	libraryObject.showNotification, err = libraryObject.dll.FindProc("PortmasterToastShow")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastShow not found %w", err)
	}

	libraryObject.setActivatedCallback, err = libraryObject.dll.FindProc("PortmasterToastActivatedCallback")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterActivatedCallback not found %w", err)
	}

	libraryObject.setDismissedCallback, err = libraryObject.dll.FindProc("PortmasterToastDismissedCallback")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastDismissedCallback not found %w", err)
	}

	libraryObject.setFailedCallback, err = libraryObject.dll.FindProc("PortmasterToastFailedCallback")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastFailedCallback not found %w", err)
	}

	libraryObject.hideNotification, err = libraryObject.dll.FindProc("PortmasterToastHide")
	if err != nil {
		return nil, fmt.Errorf("winnotifiy: PortmasterToastHide not found %w", err)
	}

	return libraryObject, nil
}

func (lib *WinToast) Initialize(appName, aumi, originalShortcutPath string) error {
	if lib == nil {
		return fmt.Errorf("wintoast: lib object was nil")
	}

	lib.Lock()
	defer lib.Unlock()

	// Initialize all necessary string for the notification meta data
	appNameUTF, _ := windows.UTF16PtrFromString(appName)
	aumiUTF, _ := windows.UTF16PtrFromString(aumi)
	linkUTF, _ := windows.UTF16PtrFromString(originalShortcutPath)

	// They are needed as unsafe pointers
	appNameP := unsafe.Pointer(appNameUTF)
	aumiP := unsafe.Pointer(aumiUTF)
	linkP := unsafe.Pointer(linkUTF)

	// Initialize notifications
	rc, _, err := lib.initialize.Call(uintptr(appNameP), uintptr(aumiP), uintptr(linkP))
	if rc != 0 {
		return fmt.Errorf("wintoast: failed to initialize library rc = %d, %w", rc, err)
	}

	// Check if if the initialization was successfully
	rc, _, _ = lib.isInitialized.Call()
	if rc == 1 {
		lib.initialized.Set()
	} else {
		return fmt.Errorf("wintoast: initialized flag was not set: rc = %d", rc)
	}

	return nil
}

func (lib *WinToast) SetCallbacks(activated func(id int64, actionIndex int32), dismissed func(id int64, reason int32), failed func(id int64, reason int32)) error {
	if lib == nil {
		return fmt.Errorf("wintoast: lib object was nil")
	}

	if lib.initialized.IsNotSet() {
		return fmt.Errorf("winnotifiy: library not initialized")
	}

	// Initialize notification activated callback
	callback := windows.NewCallback(func(id int64, actionIndex int32) uint64 {
		activated(id, actionIndex)
		return 0
	})
	rc, _, err := lib.setActivatedCallback.Call(callback)
	if rc != 1 {
		return fmt.Errorf("winnotifiy: failed to initialize activated callback %w", err)
	}

	// Initialize notification dismissed callback
	callback = windows.NewCallback(func(id int64, actionIndex int32) uint64 {
		dismissed(id, actionIndex)
		return 0
	})
	rc, _, err = lib.setDismissedCallback.Call(callback)
	if rc != 1 {
		return fmt.Errorf("winnotifiy: failed to initialize dismissed callback %w", err)
	}

	// Initialize notification failed callback
	callback = windows.NewCallback(func(id int64, actionIndex int32) uint64 {
		failed(id, actionIndex)
		return 0
	})
	rc, _, err = lib.setFailedCallback.Call(callback)
	if rc != 1 {
		return fmt.Errorf("winnotifiy: failed to initialize failed callback %s", err)
	}

	return nil
}

// NewNotification starts a creation of new notification. NotificationBuilder.Delete should allays be called when done using the object or there will be memory leeks
func (lib *WinToast) NewNotification(title string, content string) (*NotificationBuilder, error) {
	if lib == nil {
		return nil, fmt.Errorf("wintoast: lib object was nil")
	}
	return newNotification(lib, title, content)
}

// HideNotification hides notification
func (lib *WinToast) HideNotification(id int64) error {
	if lib == nil {
		return fmt.Errorf("wintoast: lib object was nil")
	}

	lib.Lock()
	defer lib.Unlock()

	rc, _, _ := lib.hideNotification.Call(uintptr(id))

	if rc != 1 {
		return fmt.Errorf("wintoast: failed to hide notification %d", id)
	}

	return nil
}
