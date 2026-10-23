package ui

import (
	"os"
	"sync/atomic"
	"testing"
)

var (
	soundRequests  atomic.Int64
	notifyRequests atomic.Int64
)

// Settings toggles preview the finish sound and ring the terminal bell.
func TestMain(m *testing.M) {
	playSound = func(string) { soundRequests.Add(1) }
	systemNotify = func() { notifyRequests.Add(1) }
	os.Exit(m.Run())
}
