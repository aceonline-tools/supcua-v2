//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/app/status"
	"github.com/yanmad27/supcua-v2/autoactive"
	"github.com/yanmad27/supcua-v2/bridge"
	"github.com/yanmad27/supcua-v2/executor"
	"github.com/yanmad27/supcua-v2/hotkey"
	imagedetector "github.com/yanmad27/supcua-v2/image"
	"github.com/yanmad27/supcua-v2/latency"
	"github.com/yanmad27/supcua-v2/license"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
	"github.com/yanmad27/supcua-v2/ui"
	"github.com/yanmad27/supcua-v2/ui/webview"
	"github.com/yanmad27/supcua-v2/ui/webview/assets"
	"github.com/yanmad27/supcua-v2/ui/webview/rpc"
	"github.com/yanmad27/supcua-v2/updater"
	"github.com/yanmad27/supcua-v2/version"
)

// lateUI lets the bridge be built before the WebView2 host exists (the host needs the
// bridge's method table). Calls before the host is set are dropped.
type lateUI struct {
	p       atomic.Pointer[webview.UI]
	onTheme func(theme string) // theme changes -> native title bar
}

func (l *lateUI) Post(f func()) {
	if u := l.p.Load(); u != nil {
		u.Post(f)
	}
}

// tryPost reports whether f was handed to the UI thread; otherwise it returns why not (the
// window is not ready yet, or the UI was closed by Quit).
func (l *lateUI) tryPost(f func()) (bool, string) {
	u := l.p.Load()
	if u == nil {
		return false, "UI window not ready"
	}
	if u.Closed() { // best effort: a Quit between this check and Post still drops silently
		return false, "UI closing"
	}
	u.Post(f)
	return true, ""
}

func (l *lateUI) Emit(event string, payload any) {
	if st, ok := payload.(bridge.StateDTO); ok && l.onTheme != nil {
		l.onTheme(st.Theme)
	}
	if u := l.p.Load(); u != nil {
		u.Emit(event, payload)
	}
}

// EmitTo implements bridge.TargetEmitter: events are routed to the main window, the editor window
// or both (editor-window contract §2.4).
func (l *lateUI) EmitTo(t bridge.Target, event string, payload any) {
	if st, ok := payload.(bridge.StateDTO); ok && l.onTheme != nil {
		l.onTheme(st.Theme)
	}
	if u := l.p.Load(); u != nil {
		u.EmitToWindows(t != bridge.TargetEditor, t != bridge.TargetMain, event, payload)
	}
}

// hwnd is the ACTIVE MODAL window: the editor while one exists, else main. Capture hooks evaluate
// it per event from their own thread (editor-window contract §8).
func (l *lateUI) hwnd() uintptr {
	if u := l.p.Load(); u != nil {
		if ed := u.Editor(); ed != nil {
			if h := ed.HWND(); h != 0 {
				return h
			}
		}
		return u.HWND()
	}
	return 0
}

func systemPrefersDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

func wantDark(theme string) bool {
	switch theme {
	case "dark":
		return true
	case "light":
		return false
	}
	return systemPrefersDark()
}

// diagKit is the frontend diagnostics channel plus the initial-load deadline.
type diagKit struct {
	diag    *ui.Diag
	watch   *ui.LoadWatch
	mounted chan struct{}
}

const initialLoadDeadline = 20 * time.Second

func newDiagKit(stages *ui.Stages, res *webview.Result, logPath string) *diagKit {
	k := &diagKit{mounted: make(chan struct{})}
	k.watch = &ui.LoadWatch{
		Timeout: initialLoadDeadline, Stage: stages.Last, Logf: log.Printf,
		OnTimeout: func(stage string) {
			if res != nil {
				res.Step("frontend mounted", false, "not mounted within "+initialLoadDeadline.String()+"; last stage: "+stage)
				os.Exit(res.Finish(1))
			}
			if k.watch.IsMounted() { // mounted between the deadline and now: nothing to warn about
				return
			}
			webview.ShowLoadTimeout(logPath, stage)
		},
	}
	k.watch.OnLate = func(time.Duration) { go webview.DismissLoadTimeout() }
	k.diag = &ui.Diag{Logf: log.Printf, OnMounted: func(route string) {
		stages.Mark("frontend mounted route=" + route)
		k.watch.Mounted()
		close(k.mounted)
	}}
	return k
}

func (k *diagKit) install(methods map[string]func(json.RawMessage) (any, error)) {
	for name, h := range k.diag.Methods() {
		methods[name] = h
	}
}

// autoActiveProxy is the bridge's AutoActive: it forwards to the watcher and keeps the tray
// checkbox in sync when the setting changes from the Settings page.
type autoActiveProxy struct {
	w       *autoactive.Watcher
	setTray func(enabled bool)
}

func (a *autoActiveProxy) Start() { a.w.Start(); a.setTray(true) }
func (a *autoActiveProxy) Stop()  { a.w.Stop(); a.setTray(false) }
func (a *autoActiveProxy) SetProcessName(n string) {
	a.w.SetProcessName(autoactive.ResolveProcessName(n))
}

func main() {
	selftestFlag := flag.Bool("selftest", false, "run the headless UI self-test and exit (0 = pass)")
	noRuntimeFlag := flag.Bool("selftest-noruntime", false, "self-test the missing-WebView2-runtime path and exit")
	prodFlag := flag.Bool("selftest-production", false, "run the production startup sequence against a fixture data dir and exit (0 = pass)")
	fixtureDir := flag.String("fixture", "", "fixture data directory copied into the throw-away data dir (-selftest-production)")
	label := flag.String("label", "plain", "build label echoed in the self-test result")
	timeout := flag.Duration("timeout", 210*time.Second, "hard self-test timeout")
	flag.Parse()

	if *selftestFlag || *noRuntimeFlag || *prodFlag {
		webview.AttachParentConsole()
	}
	if *noRuntimeFlag {
		os.Exit(webview.RunNoRuntimeSelftest(webview.NewResult(*label, "selftest-noruntime"), *timeout))
	}
	policy := ui.NewPolicy(*selftestFlag)
	var res *webview.Result
	if *prodFlag {
		policy = ui.NewProductionSelfTestPolicy()
		res = webview.NewResult(*label, "selftest-production")
		res.ArmWatchdog(*timeout)
	} else if *selftestFlag {
		res = webview.NewResult(*label, "selftest")
		res.ArmWatchdog(*timeout)
	}

	if policy.UseUserData { // single instance (the self-test never competes with a running app)
		mutexName, _ := windows.UTF16PtrFromString("Local\\supcua-v2-instance")
		mutex, err := windows.CreateMutex(nil, true, mutexName)
		if err == windows.ERROR_ALREADY_EXISTS {
			msg, _ := windows.UTF16PtrFromString("supcua-v2 is already running.")
			title, _ := windows.UTF16PtrFromString("Already Running")
			windows.MessageBox(0, msg, title, windows.MB_OK|windows.MB_ICONINFORMATION)
			os.Exit(0)
		}
		defer windows.CloseHandle(mutex)
	}

	executablePath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}
	applicationDirectory := filepath.Dir(executablePath)

	// Log to file since -H windowsgui hides console
	logFile, logErr := newRotatingWriter(filepath.Join(applicationDirectory, "supcua-v2.log"), 5<<20)
	if logErr == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
	log.Println("supcua-v2 starting...")
	stages := ui.NewStages(log.Printf)
	logPath := filepath.Join(applicationDirectory, "supcua-v2.log")

	// WebView2 runtime: required for every screen (also the not-licensed one).
	runtimeVersion, err := webview.CheckRuntime()
	if err != nil {
		log.Printf("webview2 runtime: %v", err)
		if res != nil {
			res.Step("runtime present", false, err.Error())
			os.Exit(res.Finish(1))
		}
		webview.ShowRuntimeMissing()
		os.Exit(3)
	}

	stages.Mark("webview2 runtime " + runtimeVersion)

	// License check: production only. The self-test policy has no licence capability.
	licensed, macs := true, []string(nil)
	if policy.CheckLicense {
		t0 := time.Now()
		allowed, lerr := license.CheckLicense()
		if lerr != nil {
			log.Printf("license check error: %v", lerr)
		}
		if !allowed {
			log.Println("not licensed, showing license screen")
			licensed, macs = false, license.GetMacAddresses()
		}
		stages.Mark(fmt.Sprintf("licence check: licensed=%v (%dms)", licensed, time.Since(t0).Milliseconds()))
	}

	// Every exit path runs under a hard bound: a stuck lock in a shutdown step must never leave
	// the process alive with a hidden window.
	const normalExitBound, fatalExitBound = 10 * time.Second, 5 * time.Second
	watchdog := ui.NewExitWatchdog(os.Exit)

	title := "supcua-v2 v" + version.Version
	late := &lateUI{}
	offers := &updater.OfferStore{}
	// The update check is wired only where the policy allows the network: production, or the self-test
	// whose stub must be installed first (it never reaches api.github.com).
	var updateCheck func(string) (string, string, string)
	switch {
	case policy.CheckForUpdates:
		updateCheck = updater.CheckKind
	case policy.UpdateCheckStub:
		updateCheck = func(cur string) (string, string, string) {
			if !updater.BaseIsSelftestStub() {
				return "", "", updater.KindOther
			}
			return updater.CheckKind(cur)
		}
	}
	host := &ui.Host{Offers: offers, Mode: "main", Macs: macs, Version: version.Version, RuntimeVersion: runtimeVersion, ExePath: executablePath}

	if !licensed {
		host.Mode = "notLicensed"
		var u *webview.UI
		var exiting, fatalLicense atomic.Bool
		quit := func() {
			exiting.Store(true)
			watchdog.Arm(normalExitBound, 0, "exit")
			u.Quit()
		}
		host.OnExit = quit
		licenseFatal := func(why string) { // UI thread
			watchdog.Arm(fatalExitBound, 5, "fatal exit")
			fatalLicense.Store(true)
			log.Printf("license window failure: %s: leaving the app", why)
			u.Quarantine()
			u.Post(u.Quit)
		}
		recovery := &ui.Recovery{
			Bound:    10 * time.Second,
			Arm:      func() { watchdog.Arm(fatalExitBound, 5, "fatal exit") },
			Fatal:    licenseFatal,
			Post:     func(f func()) { u.Post(f) },
			Shutting: exiting.Load,
			Enqueue:  func(job func()) bool { return u.EnqueueRPCJob(job) },
			Reload:   func() error { return u.Reload() },
			Logf:     log.Printf,
		}
		methods := ui.NotLicensedMethods(host)
		kit := newDiagKit(stages, nil, logPath)
		kit.install(methods)
		baseReady := methods["app.ready"]
		var readyLogged sync.Once
		methods["app.ready"] = func(p json.RawMessage) (any, error) {
			readyLogged.Do(func() { stages.Mark("app.ready received") })
			recovery.Ready()
			return baseReady(p)
		}
		var navLogged sync.Once
		stages.Mark("creating webview (licence screen)")
		u, err = webview.New(webview.Config{
			Title: title, Methods: methods, Assets: assets.FS(),
			DarkTitleBar: systemPrefersDark(), MinW: 480, MinH: 360,
			OnClose:         quit,
			OnProcessFailed: recovery.Failed,
			OnNavigationCompleted: func(ok bool) {
				navLogged.Do(func() { stages.Mark(fmt.Sprintf("first navigation completed success=%v", ok)) })
				recovery.Navigated(ok)
			},
			OnFatal: func(error) { // the host quarantines right after this returns; nothing to flush
				watchdog.Arm(fatalExitBound, 5, "fatal exit")
				fatalLicense.Store(true)
				u.Post(u.Quit)
			},
		})
		if err != nil {
			log.Printf("webview startup failed: %v", err)
			webview.ShowStartupFailure()
			os.Exit(4)
		}
		host.UI = u
		stages.Mark("webview created and shown")
		kit.watch.Start()
		stages.Mark("entering Run")
		u.Run()
		if fatalLicense.Load() {
			log.Println("supcua-v2 exiting: WebView2 failed")
			watchdog.Disarm()
			webview.ShowStartupFailure()
			os.Exit(5)
		}
		return
	}

	// Storage: the self-test uses a throw-away directory, never the user's config or scripts.
	dataDirectory := applicationDirectory
	if !policy.UseUserData {
		if dataDirectory, err = os.MkdirTemp("", "supcua-selftest-"); err != nil {
			res.Step("temp data dir", false, err.Error())
			os.Exit(res.Finish(1))
		}
		defer os.RemoveAll(dataDirectory)
		if *fixtureDir != "" {
			if err := ui.CopyTree(*fixtureDir, dataDirectory); err != nil {
				res.Step("seed fixture", false, err.Error())
				os.Exit(res.Finish(1))
			}
		}
	}
	configStore := storage.NewConfigStore(filepath.Join(dataDirectory, "config.json"))
	configService, err := bridge.NewConfigService(configStore)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	appConfig := configService.Get()
	stages.Mark("config loaded")

	scriptsDirectory := appConfig.ScriptsDir
	if !filepath.IsAbs(scriptsDirectory) {
		scriptsDirectory = filepath.Join(dataDirectory, scriptsDirectory)
	}
	scriptStore := storage.NewScriptStore(scriptsDirectory)

	// Core components. The executor gets the tracking wrapper so the bridge can report locks.
	var br *bridge.Bridge
	lockTracker := ui.NewLockTracker(executor.NewInputSimulator(), func() {
		if br != nil {
			br.NotifyStateChanged()
		}
	})
	imageDetectorInstance := imagedetector.NewDetector(10)
	scriptExecutor := executor.NewExecutor(lockTracker, imageDetectorInstance, appConfig.KeyPressDurationMs)
	hotkeyManager := hotkey.NewManager()
	appController := app.NewController(scriptExecutor, hotkeyManager, scriptStore, configStore)
	hotkeyManager.SetEpochSource(appController.HotkeyEpoch)

	// Allow rctrl to toggle global active even during keyboard lock
	executor.SetEmergencyToggle(appController.ToggleGlobalStatus)

	// Hotkeys. Script hotkeys are the only way scripts start, so the self-test never builds them.
	var scriptHotkeys *scriptHotkeySet
	var hotkeySet bridge.HotkeySet
	if policy.RegisterHotkeys {
		scriptHotkeys = newScriptHotkeySet(
			appController.IsGlobalActive,
			func(scripts []model.Script) []int {
				return registerScriptHotkeys(appController, hotkeyManager, scripts)
			},
			hotkeyManager.Unregister,
		)
		scriptHotkeys.Defer() // registered off the UI thread by startup, after the pump runs
		hotkeySet = scriptHotkeys
	}
	fixed := newFixedHotkeys(hotkeyManager, policy.RegisterHotkeys)

	var tray *ui.TrayIcon
	overlayCtl := &ui.OverlayController{Controller: appController, Config: configService}
	overlayCtl.OnExternalChange = func() { // UI thread: hotkey, tray, drag end
		if br != nil {
			br.RefreshOverlay()
		}
		if tray != nil {
			tray.SyncOverlayLabel()
		}
	}
	if appConfig.OverlayEnabled {
		if err := overlayCtl.SetEnabled(true); err != nil {
			ui.LogOverlayStartup(err)
		}
	}

	// Auto Active: global active follows the game window's focus
	var watcher *autoactive.Watcher
	watcher = autoactive.NewWatcher(autoactive.Options{
		ProcessName:           autoactive.ResolveProcessName(appConfig.AutoActiveProcessName),
		ForegroundProcessName: autoactive.ForegroundProcessName,
		SetActive: func(active bool, generation uint64) {
			late.Post(func() {
				if generation != watcher.Generation() {
					return
				}
				if appController.IsGlobalActive() != active {
					appController.ToggleGlobalStatus()
				}
			})
		},
	})
	proxy := &autoActiveProxy{w: watcher, setTray: func(enabled bool) {
		late.Post(func() {
			if tray != nil {
				tray.SetAutoActiveChecked(enabled)
			}
		})
	}}

	// exitFlow is the ONLY way out (X, tray Exit, app.exit, update replacement, fatal page
	// failure). UI thread. BeginShutdown tells the page and waits off-thread (the pump keeps
	// running so the page can answer), then runs `then` on the UI thread: final flush,
	// teardown, and only afterwards finalize (the update replacement). A second request while
	// one is pending is a no-op (returns false).
	//
	// Bounded UI-thread cost: `then` runs Shutdown synchronously (FlushSaves <= 2 s) and the
	// hotkey unregister (<= 1 s) on the UI thread at exit, as the pre-WebView2 app did when it
	// flushed after mainWindow.Run; the page is already told and nothing else needs the pump.
	var shutting atomic.Bool
	var flushFailed atomic.Bool
	exitFlow := func(finalize func() error) bool {
		if !shutting.CompareAndSwap(false, true) {
			return false
		}
		watchdog.Arm(normalExitBound, 0, "ordered exit")
		br.BeginShutdown(1500*time.Millisecond, func() {
			if err := br.Shutdown(2 * time.Second); err != nil {
				flushFailed.Store(true)
				log.Printf("warning: %v", err)
			}
			unregistered := make(chan struct{}) // bounded: the hotkey thread answers each Unregister
			go func() {
				fixed.stop() // nothing may register once teardown starts (also joins one in flight)
				if scriptHotkeys != nil {
					scriptHotkeys.Stop()
				}
				hotkeyManager.UnregisterAll()
				hotkeyManager.StopPreview()
				close(unregistered)
			}()
			select {
			case <-unregistered:
			case <-time.After(time.Second):
				log.Println("warning: hotkeys not unregistered before exit")
			}
			if tray != nil {
				tray.Dispose()
			}
			overlayCtl.Destroy()
			if u := late.p.Load(); u != nil {
				u.Quit() // saves window bounds, tears the host down, ends the pump
			}
			if finalize != nil {
				if err := finalize(); err != nil {
					log.Printf("update not installed: %v", err)
					return // original executable kept; normal exit, the update is offered again next launch
				}
				os.Exit(0) // the replacement script restarts the new executable
			}
		})
		return true
	}
	quitApp := func() { exitFlow(nil) }
	host.OnUpdateReady = func(d updater.Downloaded) {
		started := exitFlow(func() error {
			if flushFailed.Load() { // never replace the executable over unsaved scripts
				updater.Discard(d)
				return errors.New("pending saves were not flushed")
			}
			return updater.Replace(d, executablePath)
		})
		if !started { // another exit is already running: nothing will install this file
			updater.Discard(d)
		}
	}

	// fatal: terminal quarantine of the page (dispatcher closed for good, window hidden,
	// controller closed), a hard watchdog, then the ordered exit; Run's caller shows the
	// startup-failure box and exits 5. UI thread.
	var fatalPage atomic.Bool
	fatal := func(why string) {
		watchdog.Arm(fatalExitBound, 5, "fatal exit") // first: every later step may hang
		fatalPage.Store(true)
		log.Printf("webview failure: %s: leaving the app", why)
		if u := late.p.Load(); u != nil {
			u.Quarantine()
		}
		late.Post(quitApp)
	}

	// Process failure policy: ui.Recovery (shared with the licence window). Only a render-process
	// exit is recovered; the deadline completes only when the reloaded page loaded successfully
	// and called app.ready, else fatal. Browser exit, an unresponsive renderer and repeated
	// failures are fatal; auxiliary processes are only logged.
	recovery := &ui.Recovery{
		Bound:    10 * time.Second,
		Arm:      func() { watchdog.Arm(fatalExitBound, 5, "fatal exit") },
		Fatal:    fatal,
		Post:     late.Post,
		Shutting: shutting.Load,
		Enqueue: func(job func()) bool {
			u := late.p.Load()
			return u != nil && u.EnqueueRPCJob(job)
		},
		Reset: func() { br.ResetSession("processFailed") },
		Reload: func() error {
			u := late.p.Load()
			if u == nil {
				return errors.New("webview not ready")
			}
			return u.Reload()
		},
		Logf: log.Printf,
	}
	pageFailed := func(kind int32) {
		if kind == 0 && br != nil { // browser process exit: the editor's controller died with it, clean it up first
			if ev := br.EditorEvents(); ev != nil {
				ev.SharedBrowserFailed()
			}
		}
		recovery.Failed(kind)
	}
	host.OnExit = quitApp
	host.Rebind = fixed.rebind

	br = bridge.New(bridge.Deps{
		Controller: appController,
		Store:      scriptStore,
		Config:     configService,
		Hotkeys:    hotkeySet,
		AutoActive: proxy,
		Windows:    bridge.SystemWindowLister{},
		Locks:      lockTracker,
		Overlay:    overlayCtl,
		UI:         late,
		Capture:    &ui.CaptureBackend{HWND: late.hwnd},
		HotkeyMute: hotkeyManager,
		Host:       host,
		CallerRole: func(ctx context.Context) string {
			if c, ok := rpc.CallerFrom(ctx); ok {
				return c.Role.String()
			}
			return ""
		},
		UpdateCheck: updateCheck,
		Offers:      offers,
		NewDeferred: func() (any, func(any, error)) { d := rpc.NewDeferred(); return d, d.Resolve },
	}, late)

	initialScripts := br.InitialScripts()
	stages.Mark(fmt.Sprintf("bridge ready (%d scripts)", len(initialScripts)))

	methods := br.Methods()
	kit := newDiagKit(stages, res, logPath)
	kit.install(methods)
	ready := make(chan struct{})
	var readyOnce sync.Once
	baseReady := methods["app.ready"]
	methods["app.ready"] = func(p json.RawMessage) (any, error) {
		out, err := baseReady(p)
		recovery.Ready()
		readyOnce.Do(func() {
			stages.Mark("app.ready received")
			close(ready)
		})
		return out, err
	}
	var selftest *ui.SelfTest
	if policy.SelfTestMethods {
		selftest = &ui.SelfTest{Res: res, Overlay: overlayCtl, Ready: ready, Exit: quitApp}
		selftest.Install(methods)
	}

	var lastDark atomic.Int32 // 0 unknown, 1 light, 2 dark: apply the title bar only on change
	late.onTheme = func(theme string) {
		dark := int32(1)
		if wantDark(theme) {
			dark = 2
		}
		if lastDark.Swap(dark) == dark {
			return
		}
		if u := late.p.Load(); u != nil {
			u.SetDarkTitleBar(dark == 2)
		}
	}
	pos := appConfig.MainWindowPos
	var navLogged sync.Once
	stages.Mark("creating webview")
	roleLists := bridge.Roles()
	both := roleLists.Both
	if selftest != nil {
		both = append(append([]string(nil), both...), ui.SelfTestMethod) // the editor page's self-test driver reports too
	}
	roles := rpc.NewMatrix(both, roleLists.MainOnly, roleLists.EditorOnly, roleLists.SelftestOnly)
	if selftest != nil {
		roles = roles.GrantMain(bridge.SelftestMainAllow()) // the selftest-only gen-0 route
	}
	webUI, err := webview.New(webview.Config{
		Title:           title,
		Methods:         methods,
		CtxMethods:      br.CtxMethods(),
		Roles:           roles,
		Assets:          assets.FS(),
		DarkTitleBar:    wantDark(appConfig.Theme),
		Initial:         webview.Bounds{X: pos.X, Y: pos.Y, W: pos.Width, H: pos.Height},
		ResetSize:       appConfig.WindowLayoutStale(),
		MinW:            640,
		MinH:            420,
		OnClose:         quitApp, // X exits for real (as before); minimize hides to the tray
		OnProcessFailed: pageFailed,
		OnNavigationCompleted: func(ok bool) {
			navLogged.Do(func() { stages.Mark(fmt.Sprintf("first navigation completed success=%v", ok)) })
			recovery.Navigated(ok)
		},
		OnFatal: func(err error) { // a security handler failed; the host quarantines right after this returns
			watchdog.Arm(fatalExitBound, 5, "fatal exit")
			fatalPage.Store(true)
			late.Post(func() { fatal(err.Error()) })
		},
		OnBounds: func(b webview.Bounds) {
			want := model.WindowPos{X: b.X, Y: b.Y, Width: b.W, Height: b.H}
			if cur := configService.Get(); cur.MainWindowPos == want && !cur.WindowLayoutStale() {
				return
			}
			if err := configService.Update(func(c *model.AppConfig) {
				c.MainWindowPos = want
				c.WindowLayout = model.WindowLayoutVersion
			}); err != nil {
				log.Printf("warning: failed to save window position: %v", err)
			}
		},
		OnActivate: func(active bool) {
			if !active && !br.EditorOpen() { // while the editor owns capture, main losing focus is not a blur
				br.NotifyBlur() // ends an active capture (contract §9.5)
			}
		},
	})
	if err != nil {
		log.Printf("webview startup failed: %v", err)
		if res != nil {
			res.Step("startup", false, err.Error())
			os.Exit(res.Finish(1))
		}
		webview.ShowStartupFailure()
		os.Exit(4)
	}
	late.p.Store(webUI)
	edHost := &ui.EditorHost{U: webUI, Events: br.EditorEvents, Blur: br.NotifyBlur}
	br.AttachEditorHost(edHost, nil, filepath.Join(dataDirectory, "recovered"))
	host.UI = webUI
	stages.Mark("webview created and shown")
	kit.watch.Start()
	if fatalPage.Load() { // a security handler failed during startup, before Post could reach the UI
		late.Post(func() { fatal("security handler failed during startup") })
	}

	tray, err = ui.NewTrayIcon(webUI.MainWindow(), webUI, appController, overlayCtl, quitApp)
	if err != nil {
		log.Printf("failed to create tray icon: %v", err)
		if res != nil {
			res.Step("tray", false, err.Error())
			os.Exit(res.Finish(1))
		}
		os.Exit(1)
	}
	defer tray.Dispose()
	stages.Mark("tray done")
	tray.AddAutoActiveToggle(appConfig.AutoActiveEnabled, func(enabled bool) error {
		if err := configService.Update(func(c *model.AppConfig) { c.AutoActiveEnabled = enabled }); err != nil {
			log.Printf("warning: failed to save auto active setting: %v", err)
			return err
		}
		if enabled {
			watcher.Start()
		} else {
			watcher.Stop()
		}
		br.NotifyStateChanged()
		return nil
	})
	tray.AddAutoActiveProcessItem(func() { br.NavGoto("settings") })
	appController.Subscribe(func(snap status.Snapshot) {
		latency.Mark(latency.Publish)
		ui.LogOverlayState(snap.GlobalActive, snap.RunningScriptName)
		// The overlay repaint is posted before Reconcile: Reconcile blocks on the hotkey listener
		// whenever the global state flips and must not delay what the user is looking at.
		ok, why := late.tryPost(func() {
			latency.Mark(latency.Refresh)
			overlayCtl.Refresh()
			tray.UpdateTooltip()
		})
		if !ok {
			ui.LogOverlayDropped(why)
		}
		if scriptHotkeys != nil {
			scriptHotkeys.Reconcile()
		}
	})
	tray.UpdateTooltip()

	// RegisterHotKey itself always runs on the hotkey listener's locked OS thread
	// (hotkey.Manager.ListenForHotkeys), which also receives WM_HOTKEY. Only the CALLER that waits
	// for the answer moves off the UI thread: the registrations below run on their own goroutine,
	// started once the pump runs, each wait bounded by hotkey.RegisterTimeout, so a stuck listener
	// can no longer leave the window unpumped.
	hotkeysDone := make(chan struct{})
	if policy.RegisterHotkeys {
		go hotkeyManager.ListenForHotkeys() // before any Register: it services the channel
		stages.Mark("hotkey listener started")
		fixed.setPreview(bridge.HotkeyGlobal, hotkey.Preview{Begin: appController.BeginTogglePreview, Cancel: appController.CancelPreview})
		appController.SetPreviewNotifier(func() {
			late.tryPost(overlayCtl.Refresh)
		})
		hotkeyManager.StartPreview()
		fixed.add(bridge.HotkeyGlobal, appConfig.GlobalHotkey, func(_ int, epoch uint64) { appController.ToggleGlobalFromHotkey(epoch) })
		fixed.add(bridge.HotkeyOverlay, appConfig.OverlayHotkey, func(_ int, epoch uint64) {
			late.Post(func() { appController.RunHotkeyAction(epoch, overlayCtl.ToggleVisibility) })
		})
		registerStartupHotkeys := func() {
			t0 := time.Now()
			fixed.start() // the LATEST desired hotkeys: rebinds made before this ran are honoured
			stages.Mark(fmt.Sprintf("fixed hotkeys registered (%d, %dms)", fixed.count(), time.Since(t0).Milliseconds()))
			t1 := time.Now()
			scriptHotkeys.Start(func() []model.Script {
				if br.InitialScriptsOK() {
					return initialScripts
				}
				scripts, err := scriptStore.List()
				if err != nil {
					log.Printf("warning: script list for hotkeys failed: %v", err)
				}
				return scripts
			})
			stages.Mark(fmt.Sprintf("script hotkeys set (%d registered, %dms)", scriptHotkeys.Count(), time.Since(t1).Milliseconds()))
			close(hotkeysDone)
		}
		webUI.Post(func() { go registerStartupHotkeys() })
	}
	if policy.StartAutoActive && appConfig.AutoActiveEnabled {
		watcher.Start()
		stages.Mark("auto-active started")
	}

	if policy.CheckForUpdates {
		go func() {
			seq := offers.Begin()
			latestTag, dlURL, err := updater.CheckUpdateShared(version.Version)
			if err != nil {
				log.Printf("update check error: %v", err)
				return
			}
			if latestTag == "" {
				offers.Publish(seq, updater.Offer{})
				return
			}
			if dlURL == "" {
				log.Printf("update %s available but no download URL; not offering it", latestTag)
				return
			}
			log.Printf("update available: %s", latestTag)
			if !offers.Publish(seq, updater.Offer{Version: latestTag, URL: dlURL}) {
				log.Printf("update %s superseded by a newer check; not offering it", latestTag)
				return
			}
			<-webUI.Loaded()
			time.Sleep(2 * time.Second)                // let the page subscribe before the event
			if offers.Current().Version != latestTag { // a newer check replaced it, or it was skipped
				return
			}
			br.NotifyUpdateAvailable(latestTag)
		}()
	}

	if selftest != nil {
		selftest.UI, selftest.Tray = webUI, tray
		selftest.Host, selftest.RecoveredDir = edHost, filepath.Join(dataDirectory, "recovered")
		selftest.EditOpen = func() bool { return appController.Snapshot().EditDialogOpen }
		if *prodFlag {
			probe := model.Hotkey{Modifiers: []string{"ctrl", "alt", "shift"}, Key: "f11"}
			go selftest.RunProduction(ui.ProductionHooks{
				Mounted: kit.mounted, HotkeysDone: hotkeysDone,
				FixedRegistered: fixed.count, ScriptHotkeyCount: scriptHotkeys.Count,
				GlobalActive: appController.IsGlobalActive, RunningScriptID: appController.GetRunningScriptID,
				GlobalHotkey: appConfig.GlobalHotkey, ProbeHotkey: probe, ProbeID: "fx-probe-1",
				ExpectFixed: 2, AutoActiveEnabled: appConfig.AutoActiveEnabled, ExpectRow: "Fixture Farm",
				OverlayHotkey: appConfig.OverlayHotkey,
				Register: func(hk model.Hotkey, f func(uint64), begin func(uint64) uint64) (int, error) {
					id, err := hotkeyManager.Register(hk, func(_ int, epoch uint64) { f(epoch) })
					if err == nil && begin != nil {
						hotkeyManager.SetPreview(id, hotkey.Preview{Begin: begin, Cancel: appController.CancelPreview})
					}
					return id, err
				},
				PreviewToggle: appController.BeginTogglePreview,
				PreviewStop:   func(epoch uint64) uint64 { return appController.BeginStopPreview("fx-probe-1", epoch) },
				Unregister:    func(id int) { _ = hotkeyManager.Unregister(id) },
				ToggleGlobal:  appController.ToggleGlobalFromHotkey,
				StopRunning:   appController.StopRunningScriptFromHotkey,
				PhysicalRCtrl: executor.SimulatePhysicalRCtrl,
			})
		} else {
			go selftest.Run()
		}
	}

	stages.Mark("entering Run")
	webUI.Run() // walk message loop: tray, Synchronize, WebView2
	watcher.Stop()
	if fatalPage.Load() {
		log.Println("supcua-v2 exiting: WebView2 failed")
		watchdog.Disarm() // teardown is complete; the box waits for the user, then exit 5
		webview.ShowStartupFailure()
		os.Exit(5)
	}

	log.Println("supcua-v2 exiting")
	if selftest != nil {
		selftest.AfterRun()
	}
	if res != nil {
		os.RemoveAll(dataDirectory)
		os.Exit(res.Finish(0))
	}
}
