//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static void hrdxRequestAttention(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (![NSApp isActive]) {
			[NSApp requestUserAttention:NSInformationalRequest];
		}
	});
}
*/
import "C"

// attentionSupported reports that the dock attention request works here.
const attentionSupported = true

// requestAttention bounces the dock icon once while the app is inactive. It
// is the desktop counterpart of the TUI's terminal bell.
func requestAttention() { C.hrdxRequestAttention() }
