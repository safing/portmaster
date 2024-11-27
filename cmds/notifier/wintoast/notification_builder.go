//go:build windows

package wintoast

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type NotificationBuilder struct {
	templatePointer uintptr
	lib             *WinToast
}

func newNotification(lib *WinToast, title string, message string) (*NotificationBuilder, error) {
	lib.Lock()
	defer lib.Unlock()

	titleUTF, _ := windows.UTF16PtrFromString(title)
	messageUTF, _ := windows.UTF16PtrFromString(message)
	titleP := unsafe.Pointer(titleUTF)
	messageP := unsafe.Pointer(messageUTF)

	ptr, _, err := lib.createNotification.Call(uintptr(titleP), uintptr(messageP))
	if ptr == 0 {
		return nil, err
	}

	return &NotificationBuilder{ptr, lib}, nil
}

func (n *NotificationBuilder) Delete() {
	if n == nil {
		return
	}

	n.lib.Lock()
	defer n.lib.Unlock()

	_, _, _ = n.lib.deleteNotification.Call(n.templatePointer)
}

func (n *NotificationBuilder) AddButton(text string) error {
	n.lib.Lock()
	defer n.lib.Unlock()
	textUTF, _ := windows.UTF16PtrFromString(text)
	textP := unsafe.Pointer(textUTF)

	rc, _, err := n.lib.addButton.Call(n.templatePointer, uintptr(textP))
	if rc != 1 {
		return err
	}
	return nil
}

func (n *NotificationBuilder) SetImage(iconPath string) error {
	n.lib.Lock()
	defer n.lib.Unlock()
	pathUTF, _ := windows.UTF16PtrFromString(iconPath)
	pathP := unsafe.Pointer(pathUTF)

	rc, _, err := n.lib.setImage.Call(n.templatePointer, uintptr(pathP))
	if rc != 1 {
		return err
	}
	return nil
}

func (n *NotificationBuilder) SetSound(option int, path int) error {
	n.lib.Lock()
	defer n.lib.Unlock()

	rc, _, err := n.lib.setSound.Call(n.templatePointer, uintptr(option), uintptr(path))
	if rc != 1 {
		return err
	}
	return nil
}

func (n *NotificationBuilder) Show() (int64, error) {
	n.lib.Lock()
	defer n.lib.Unlock()

	id, _, err := n.lib.showNotification.Call(n.templatePointer)
	if int64(id) == -1 {
		return -1, err
	}
	return int64(id), nil
}
