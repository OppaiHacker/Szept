//go:build android

package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const mobile = true

// In c-shared mode main() is not called by the runtime; the Java host starts
// it through Wails once the bridge is up.
func init() { application.RegisterAndroidMain(main) }

func filesDir() string {
	d := application.Android.StoragePath()
	if d == "" {
		// a relative path would put the identity key wherever the process sits
		panic("android: no app storage path")
	}
	return d
}

// dirs: the same place the Fyne build kept its config and data
// (getFilesDir()/fyne/<app id>), so installing this over it keeps the identity.
func dirs() (cfgFile, dataDir string) {
	root := filepath.Join(filesDir(), "fyne", "chat.szept")
	return filepath.Join(root, "config.json"), filepath.Join(root, "data")
}

// outDir stages files on their way out of the sandbox (saved attachments,
// backups); the Java host moves them to Downloads and deletes the copy.
func outDir() (string, error) {
	d := filepath.Join(filesDir(), "out")
	return d, os.MkdirAll(d, 0o700)
}

// notifier: Android's NotificationManager through the Wails bridge. The D-Bus
// service the desktop uses would fail to start here and take the app down.
func notifier() ([]application.Service, func(id, title, body string) error) {
	return nil, func(id, title, body string) error {
		b, _ := json.Marshal(map[string]string{"id": id, "title": title, "body": body})
		application.Android.Notify(string(b))
		return nil
	}
}

// lifecycle: Go takes over ping notifications while the activity is stopped.
func lifecycle(app *application.App, api *App) {
	app.Event.OnApplicationEvent(events.Android.ActivityStopped, func(*application.ApplicationEvent) { api.setBG(true) })
	app.Event.OnApplicationEvent(events.Android.ActivityStarted, func(*application.ApplicationEvent) { api.setBG(false) })
}
