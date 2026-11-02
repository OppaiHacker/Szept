//go:build !android

package main

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"szept/internal/boot"
)

const mobile = false

func dirs() (cfgFile, dataDir string) { return boot.Dirs() }

func outDir() (string, error) { return "", errors.New("desktop saves through a dialog") }

func notifier() ([]application.Service, func(id, title, body string) error) {
	n := notifications.New()
	return []application.Service{application.NewService(n)}, func(id, title, body string) error {
		return n.SendNotification(notifications.NotificationOptions{ID: id, Title: title, Body: body})
	}
}

func lifecycle(*application.App, *App) {}
