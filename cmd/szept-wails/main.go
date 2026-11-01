// szept-wails: desktop client rendered by a webview (Wails v3) instead of Fyne.
//
// The whole UI lives in frontend/ as plain HTML/CSS/JS — no bundler, no npm.
// Go keeps doing what it did before: it owns core and exposes it through the
// App service (api.go) plus a few HTTP routes for bytes too big for the
// JSON bridge (media.go).
package main

import (
	"embed"
	_ "embed"
	"flag"
	"log"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"szept/internal/boot"
	"szept/internal/core"
)

//go:embed all:frontend
var assets embed.FS

//go:embed Icon.png
var iconPNG []byte

func main() {
	dataFlag := flag.String("data", "", "data directory")
	connFlag := flag.String("connector", "", "connector address")
	devtools := flag.Bool("devtools", false, "open the webview inspector")
	flag.Parse()

	// Wails' init() sets this to "1" on every NVIDIA GPU (old drivers showed
	// blank windows). Without DMA-BUF WebKit copies each frame through the CPU
	// and all scrolling lags; current drivers handle it. To opt out, set any
	// other non-"0" value, e.g. WEBKIT_DISABLE_DMABUF_RENDERER=yes.
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "1" {
		os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "0")
	}
	// WebKit's GL video sink draws every frame solid green on NVIDIA
	// (checked on an RTX 5080, Wayland); the plain sink draws it right.
	// Opt back in with WEBKIT_GST_DISABLE_GL_SINK=0.
	if os.Getenv("WEBKIT_GST_DISABLE_GL_SINK") == "" {
		os.Setenv("WEBKIT_GST_DISABLE_GL_SINK", "1")
	}

	cfgFile, dataDir := boot.Dirs()
	if *dataFlag != "" {
		dataDir = *dataFlag
	}
	cfg, _ := boot.LoadConfig(cfgFile)
	if cfg == nil {
		cfg = &core.Config{}
	}
	if *connFlag != "" {
		cfg.Connector = *connFlag
	}
	cfg.DataDir = dataDir

	api := newApp(cfg, cfgFile, dataDir, *dataFlag != "")
	notifier := notifications.New()
	api.notify = notifier

	app := application.New(application.Options{
		Name:        "szept",
		Description: "szyfrowany komunikator P2P",
		Icon:        iconPNG,
		LogLevel:    slog.LevelWarn,
		Services: []application.Service{
			application.NewService(api),
			application.NewService(notifier),
		},
		Assets: application.AssetOptions{
			Handler:        application.BundledAssetFileServer(assets),
			Middleware:     api.routes,
			DisableLogging: true,
		},
		OnShutdown: api.shutdown,
	})
	api.app = app

	// Frameless: the title bar is drawn in HTML (see .titlebar) so the window
	// chrome matches the app instead of the desktop theme.
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "szept",
		Width:            1180,
		Height:           760,
		MinWidth:         360,
		MinHeight:        520,
		Frameless:        true,
		URL:              "/",
		BackgroundColour: application.NewRGB(15, 16, 20),
		// the inspector is reachable only through the native context menu,
		// which the frontend suppresses unless developer mode is on
		DevToolsEnabled: *devtools || cfg.DevMode,
		// GTK grabs file drags before the page sees them; Wails hands us the
		// paths instead (see filesDropped)
		EnableFileDrop: true,
		Linux: application.LinuxWindow{
			Icon:             iconPNG,
			WebviewGpuPolicy: application.WebviewGpuPolicyOnDemand,
		},
	})
	api.win = win

	// The frontend asks for its first snapshot once the DOM is up; nothing to
	// do here beyond making sure a late-arriving core still repaints.
	win.RegisterHook(events.Common.WindowDidResize, func(*application.WindowEvent) {})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		api.filesDropped(e.Context().DroppedFiles())
	})

	go api.updates()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
