//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework UserNotifications
#include <stdlib.h>
#include "notify_darwin.h"
*/
import "C"

import "unsafe"

// notificationsSupported reports native banners and the Dock badge.
const notificationsSupported = true

// notificationClick receives the pane id of a clicked banner.
var notificationClick func(paneID int)

//export hrdxNotificationClicked
func hrdxNotificationClicked(paneID C.int) {
	if notificationClick != nil {
		go notificationClick(int(paneID))
	}
}

// setupNotifications installs the click handler and asks for permission
// once. Outside an app bundle (go run) it does nothing.
func setupNotifications(onClick func(paneID int)) {
	notificationClick = onClick
	C.hrdxNotifySetup()
}

// showNotification posts a banner. A newer banner for the same pane replaces
// the older one.
func showNotification(title, body string, paneID int) {
	cTitle, cBody := C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cBody))
	C.hrdxNotify(cTitle, cBody, C.int(paneID))
}

// setBadge shows count on the Dock icon; zero clears it.
func setBadge(count int) { C.hrdxBadge(C.int(count)) }
