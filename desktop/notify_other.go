//go:build !darwin

package main

// Native banners and badges are macOS only for now.
const notificationsSupported = false

func setupNotifications(func(paneID int)) {}

func showNotification(string, string, int) {}

func setBadge(int) {}
