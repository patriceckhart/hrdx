//go:build !darwin

package main

// attentionSupported is false: Linux and Windows attention requests are a
// later phase, and the settings dialog says so.
const attentionSupported = false

func requestAttention() {}
