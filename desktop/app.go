package main

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/payam-foundation/dotvpn-client/internal/ipc"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	uniqueID   = "org.dotvpn.desktop"
	inviteLink = "dotvpn://"
	// backgroundFlag is what the OS login item passes so the app starts in
	// the tray instead of in front of the user.
	backgroundFlag = "--background"
)

// InviteResult is the outcome of importing a dotvpn:// link the OS handed us.
type InviteResult struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// desktop ties the window, the tray and the helper connection together. It
// holds no VPN logic: state comes from the helper and goes to the tray and the
// frontend.
type desktop struct {
	app   *application.App
	win   *application.WebviewWindow
	tray  *trayView
	dock  *dock.DockService
	notes *notifications.NotificationService
	ipc   *ipc.Client
	prefs *prefStore
	log   *slog.Logger

	wake chan struct{}

	mu      sync.Mutex
	helper  bool
	status  ipc.Status
	pending *InviteResult
}

func (d *desktop) lang() string {
	if l := d.prefs.get().Language; l != "" {
		return l
	}
	return "en"
}

func (d *desktop) watchNow() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// watch follows the helper for the life of the app. A missing helper is a
// state, not an error: the tray and the window say so, and we keep trying.
func (d *desktop) watch(ctx context.Context) {
	var prev ipc.State
	for ctx.Err() == nil {
		ch, err := d.ipc.Subscribe(ctx)
		if err != nil {
			d.setHelper(false)
			select {
			case <-ctx.Done():
				return
			case <-d.wake:
			case <-time.After(2 * time.Second):
			}
			continue
		}
		d.setHelper(true)
		prev = ""
		for st := range ch {
			d.onStatus(prev, st)
			prev = st.State
		}
		d.setHelper(false)
	}
}

func (d *desktop) setHelper(up bool) {
	d.mu.Lock()
	changed := d.helper != up
	d.helper = up
	if !up {
		d.status = ipc.Status{}
	}
	st := d.status
	d.mu.Unlock()
	if changed {
		d.app.Event.Emit("helper", up)
		d.refreshTray(up, st)
	}
}

func (d *desktop) onStatus(prev ipc.State, st ipc.Status) {
	d.mu.Lock()
	d.status = st
	d.mu.Unlock()
	d.app.Event.Emit("status", st)
	d.refreshTray(true, st)
	d.notify(prev, st)
}

// notify tells the user about the two moments they would otherwise miss while
// the window is hidden: the route changing and the connection failing.
func (d *desktop) notify(prev ipc.State, st ipc.Status) {
	lang := d.lang()
	var body string
	switch {
	case prev == ipc.StateConnected && st.State == ipc.StateSwitching:
		body = tr(lang, "fallback")
		if st.KillSwitch {
			body += " " + tr(lang, "blocked")
		}
	case prev != "" && prev != ipc.StateFailed && st.State == ipc.StateFailed:
		body = tr(lang, "failedBody")
	default:
		return
	}
	if d.notes == nil {
		return
	}
	err := d.notes.SendNotification(notifications.NotificationOptions{
		ID:    "dotvpn-" + string(st.State),
		Title: tr(lang, "notifyTitle"),
		Body:  body,
	})
	if err != nil {
		d.log.Debug("notification not delivered", "err", err)
	}
}

func (d *desktop) refreshTray(up bool, st ipc.Status) {
	d.tray.update(d.lang(), up, st)
}

func (d *desktop) showWindow() {
	if runtime.GOOS == "darwin" {
		d.dock.ShowAppIcon()
	}
	d.win.Show()
	d.win.UnMinimise()
	d.win.Focus()
}

func (d *desktop) hideWindow() {
	d.win.Hide()
	if runtime.GOOS == "darwin" {
		d.dock.HideAppIcon()
	}
}

func (d *desktop) trayOK() bool { return d.tray.supported }

// handleInvite imports a dotvpn:// link the OS opened us with and shows the
// result, so clicking a link from a chat is the whole flow.
func (d *desktop) handleInvite(uri string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res := &InviteResult{OK: true}
	if err := d.ipc.ImportInvite(ctx, uri); err != nil {
		he := toErr(err).(*helperError)
		res = &InviteResult{Code: he.Code, Message: he.Message}
	}
	d.mu.Lock()
	d.pending = res
	d.mu.Unlock()
	d.app.Event.Emit("invite", res)
	d.showWindow()
}

func (d *desktop) takeInvite() *InviteResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.pending
	d.pending = nil
	return r
}

// inviteFrom picks the dotvpn:// link out of a command line.
func inviteFrom(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, inviteLink) {
			return a
		}
	}
	return ""
}

func (d *desktop) setPrefs(p Prefs) error {
	if err := d.prefs.set(p); err != nil {
		return err
	}
	if p.StartAtLogin {
		err := d.app.Autostart.EnableWithOptions(application.AutostartOptions{
			Identifier: uniqueID,
			Arguments:  []string{backgroundFlag},
		})
		if err != nil {
			return err
		}
	} else if err := d.app.Autostart.Disable(); err != nil {
		return err
	}
	d.mu.Lock()
	up, st := d.helper, d.status
	d.mu.Unlock()
	d.refreshTray(up, st)
	d.app.Event.Emit("prefs", p)
	return nil
}

func (d *desktop) quit() { d.app.Quit() }

func (d *desktop) connectOrDisconnect() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d.mu.Lock()
	st, up := d.status, d.helper
	d.mu.Unlock()
	switch {
	case !up || !st.Configured:
		d.showWindow()
	case st.Running:
		_ = d.ipc.Disconnect(ctx)
	default:
		if err := d.ipc.Connect(ctx); err != nil {
			d.log.Warn("connect from tray", "err", err)
			d.showWindow()
		}
	}
}

func run(startHidden bool, args []string) error {
	log := slog.Default()
	d := &desktop{
		ipc:   &ipc.Client{},
		prefs: openPrefs(),
		log:   log,
		wake:  make(chan struct{}, 1),
		dock:  dock.New(),
	}
	if canNotify() {
		d.notes = notifications.New()
	}
	d.tray = newTrayView()

	services := []application.Service{application.NewService(&AppService{d: d})}
	if d.notes != nil {
		services = append(services, application.NewService(d.notes))
	}
	d.app = application.New(application.Options{
		Name:         "Dot VPN",
		Description:  "Your message gets through.",
		Icon:         appIcon,
		Services:     services,
		Assets:       application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		MarshalError: marshalError,
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyRegular,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: uniqueID,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if uri := inviteFrom(data.Args); uri != "" {
					go d.handleInvite(uri)
					return
				}
				d.showWindow()
			},
		},
	})

	bg := application.NewRGB(255, 255, 255)
	if d.app.Env.IsDarkMode() {
		bg = application.NewRGB(0, 0, 0)
	}
	hidden := startHidden && d.prefs.get().StartMinimized && d.tray.supported
	d.win = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Dot VPN",
		URL:              "/",
		Width:            420,
		Height:           720,
		MinWidth:         360,
		MinHeight:        560,
		BackgroundColour: bg,
		Hidden:           hidden,
	})
	// Closing the window leaves the app in the tray. Without a tray there is
	// nothing to come back to, so then closing quits; the helper keeps the
	// tunnel either way.
	d.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if d.tray.supported {
			e.Cancel()
			d.hideWindow()
		}
	})
	if runtime.GOOS == "darwin" {
		d.app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
			d.showWindow()
		})
	}
	d.app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(e *application.ApplicationEvent) {
		if u := e.Context().URL(); strings.HasPrefix(u, inviteLink) {
			go d.handleInvite(u)
		}
	})

	d.tray.install(d)
	if hidden && runtime.GOOS == "darwin" {
		d.dock.HideAppIcon()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go d.watch(ctx)
		if d.notes != nil {
			go func() { _, _ = d.notes.RequestNotificationAuthorization() }()
		}
		if uri := inviteFrom(args); uri != "" {
			go d.handleInvite(uri)
		}
	})
	return d.app.Run()
}
