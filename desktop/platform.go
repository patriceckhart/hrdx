package main

import (
	"context"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsPlatform implements bridge.Platform on the Wails runtime. Calls before
// the webview context exists are dropped; the frontend fetches a full
// Snapshot on mount, so no state is lost.
type wailsPlatform struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (p *wailsPlatform) SetContext(ctx context.Context) {
	p.mu.Lock()
	p.ctx = ctx
	p.mu.Unlock()
}

func (p *wailsPlatform) context() context.Context {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ctx
}

func (p *wailsPlatform) Emit(event string, payload any) {
	if ctx := p.context(); ctx != nil {
		wailsruntime.EventsEmit(ctx, event, payload)
	}
}

func (p *wailsPlatform) PickDirectory() (string, error) {
	ctx := p.context()
	if ctx == nil {
		return "", nil
	}
	return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{
		Title: "Open workspace", CanCreateDirectories: true,
	})
}

func (p *wailsPlatform) ClipboardWrite(text string) error {
	if ctx := p.context(); ctx != nil {
		return wailsruntime.ClipboardSetText(ctx, text)
	}
	return nil
}

func (p *wailsPlatform) ClipboardRead() (string, error) {
	if ctx := p.context(); ctx != nil {
		return wailsruntime.ClipboardGetText(ctx)
	}
	return "", nil
}

func (p *wailsPlatform) OpenURL(url string) {
	if ctx := p.context(); ctx != nil {
		wailsruntime.BrowserOpenURL(ctx, url)
	}
}

func (p *wailsPlatform) Quit() {
	if ctx := p.context(); ctx != nil {
		wailsruntime.Quit(ctx)
	}
}

func (p *wailsPlatform) Focus() {
	if ctx := p.context(); ctx != nil {
		wailsruntime.WindowUnminimise(ctx)
		wailsruntime.WindowShow(ctx)
	}
}

func (p *wailsPlatform) AttentionSupported() bool {
	return attentionSupported || notificationsSupported
}

func (p *wailsPlatform) RequestAttention() { requestAttention() }
