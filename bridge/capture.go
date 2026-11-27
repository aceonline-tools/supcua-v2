package bridge

import (
	"sync"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

type CaptureMode string

const (
	CaptureNone   CaptureMode = ""
	CaptureHotkey CaptureMode = "hotkey"
	CaptureKey    CaptureMode = "key"
	CaptureMouse  CaptureMode = "mouse"
	// CaptureSettingsHotkey is the main window's Settings recorder: like CaptureHotkey, but a
	// modifier tapped alone is reported with its side, global hotkeys are muted while it runs,
	// and it reports on the settings.capture.* events (the editor-only capture.* events and
	// their routing stay untouched).
	CaptureSettingsHotkey CaptureMode = "settingsHotkey"
)

// Capture end reasons (contract §9.5).
const (
	EndStopped      = "stopped"
	EndBlur         = "blur"
	EndTimeout      = "timeout"
	EndEditorClosed = "editorClosed"
)

// Events of the Settings recorder (routed to the main window).
const (
	EvSettingsCaptureHotkey = "settings.capture.hotkey"
	EvSettingsCaptureEnded  = "settings.capture.ended"
)

// settingsCaptureSession owns the Settings recorder inside the one-session capture manager. It is
// not an editor session id, so an editor window can never stop it and it can never stop an editor capture.
const settingsCaptureSession = "settings"

const defaultCaptureIdle = 60 * time.Second

// CaptureSink receives backend events. Methods may be called from any goroutine; events from a
// stopped session are dropped by the manager.
type CaptureSink interface {
	Hotkey(hk model.Hotkey)
	Key(key string)
	// KeyWithMods is Key plus the modifiers held at the key-down (non-nil, possibly empty).
	KeyWithMods(key string, mods []string)
	Mouse(x, y int)
}

// CaptureBackend is the platform hook (Windows implementation provided by the host, on its own
// locked thread). Start must not block; Stop must remove the hook/ticker and be idempotent.
type CaptureBackend interface {
	Start(mode CaptureMode, pickKey model.Hotkey, sink CaptureSink) error
	Stop()
}

// CaptureManager is the §9.5 state machine: one session at a time, no per-dialog goroutines.
// opMu serialises every backend Start/Stop with the state change that goes with it; mu guards the
// fields and is never held across a backend call. Teardown is generation-specific, and the idle
// timer re-checks the generation and the deadline, so a late timer never ends a newer capture.
type CaptureManager struct {
	backend CaptureBackend
	emit    func(event string, payload any)
	idle    time.Duration
	// suspend mutes (true) / restores (false) the app's global hotkeys; called only for the
	// Settings recorder, under opMu, so mute and restore are strictly paired with Start and teardown.
	suspend func(bool)

	opMu     sync.Mutex
	mu       sync.Mutex
	gen      uint64
	ids      uint64 // monotonically increasing capture_id source
	id       uint64 // capture_id of the active capture
	mode     CaptureMode
	session  string
	field    string
	deadline time.Time
	timer    *time.Timer
}

func newCaptureManager(backend CaptureBackend, emit func(string, any), idle time.Duration) *CaptureManager {
	if idle <= 0 {
		idle = defaultCaptureIdle
	}
	return &CaptureManager{backend: backend, emit: emit, idle: idle}
}

// Event payloads (every capture event carries the capture_id it belongs to).
type endedEvent struct {
	CaptureID uint64 `json:"capture_id"`
	Reason    string `json:"reason"`
}

type hotkeyEvent struct {
	CaptureID uint64       `json:"capture_id"`
	Field     string       `json:"field"`
	Hotkey    model.Hotkey `json:"hotkey"`
	Display   string       `json:"display"`
}

type keyEvent struct {
	CaptureID uint64 `json:"capture_id"`
	Key       string `json:"key"`
	// Mods is the modifier state at the key-down; null when the backend does not report it.
	Mods []string `json:"mods"`
}

type mouseEvent struct {
	CaptureID uint64 `json:"capture_id"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
}

// Active reports the current mode and session.
func (m *CaptureManager) Active() (CaptureMode, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mode, m.session
}

// Start begins a capture, ending any previous one with reason "stopped".
func (m *CaptureManager) Start(session string, mode CaptureMode, field string, pickKey model.Hotkey) (uint64, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.endLocked(0, EndStopped, true, false)
	if m.backend == nil {
		return 0, newErr(CodeInternal)
	}
	m.mu.Lock()
	m.gen++
	gen := m.gen
	m.ids++
	id := m.ids
	m.id = id
	m.mode, m.session, m.field = mode, session, field
	m.armLocked(gen)
	m.mu.Unlock()
	if mode == CaptureSettingsHotkey && m.suspend != nil {
		m.suspend(true)
	}
	if err := m.backend.Start(mode, pickKey, &sink{m: m, gen: gen, id: id}); err != nil {
		m.endLocked(gen, "", false, false)
		return 0, newErr(CodeInternal)
	}
	return id, nil
}

// CurrentID returns the active capture_id, or 0 when idle.
func (m *CaptureManager) CurrentID() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mode == CaptureNone {
		return 0
	}
	return m.id
}

// End stops the active capture (if any), removes the hook and emits capture.ended{reason}.
func (m *CaptureManager) End(reason string) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.endLocked(0, reason, true, false)
}

// expire is the idle-timer path: it ends generation gen only if its deadline really passed.
// EndSession ends the active capture only when it belongs to session: an older editor window's
// cleanup must never end a capture started by its replacement.
func (m *CaptureManager) EndSession(session, reason string) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	mine := m.session == session && session != ""
	m.mu.Unlock()
	if mine {
		m.endLocked(0, reason, true, false)
	}
}

func (m *CaptureManager) expire(gen uint64) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.endLocked(gen, EndTimeout, true, true)
}

// endLocked tears down generation gen (0 = whatever is active). opMu must be held.
func (m *CaptureManager) endLocked(gen uint64, reason string, notify, needExpired bool) {
	m.mu.Lock()
	if m.mode == CaptureNone || (gen != 0 && m.gen != gen) || (needExpired && time.Now().Before(m.deadline)) {
		m.mu.Unlock()
		return
	}
	m.gen++
	endedID, endedMode := m.id, m.mode
	m.mode, m.session, m.field = CaptureNone, "", ""
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	if m.backend != nil {
		m.backend.Stop()
	}
	if endedMode == CaptureSettingsHotkey {
		if m.suspend != nil {
			m.suspend(false)
		}
		if notify {
			m.emit(EvSettingsCaptureEnded, endedEvent{CaptureID: endedID, Reason: reason})
		}
		return
	}
	if notify {
		m.emit(EvCaptureEnded, endedEvent{CaptureID: endedID, Reason: reason})
	}
}

// armLocked (re)starts the single idle timer; mu held.
func (m *CaptureManager) armLocked(gen uint64) {
	m.deadline = time.Now().Add(m.idle)
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(m.idle, func() { m.expire(gen) })
}

// touch validates the event's generation and refreshes the idle deadline.
func (m *CaptureManager) touch(gen uint64, want CaptureMode) (field string, ok bool) {
	f, _, ok := m.touchMode(gen, want, want)
	return f, ok
}

// touchMode is touch for an event either of two modes may produce; it reports the active mode.
func (m *CaptureManager) touchMode(gen uint64, a, b CaptureMode) (field string, mode CaptureMode, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen != gen || m.mode == CaptureNone || (a != CaptureNone && m.mode != a && m.mode != b) {
		return "", CaptureNone, false
	}
	m.armLocked(gen)
	return m.field, m.mode, true
}

type sink struct {
	m   *CaptureManager
	gen uint64
	id  uint64
}

func (s *sink) Hotkey(hk model.Hotkey) {
	field, mode, ok := s.m.touchMode(s.gen, CaptureHotkey, CaptureSettingsHotkey)
	if !ok {
		return
	}
	if mode == CaptureSettingsHotkey {
		s.m.emit(EvSettingsCaptureHotkey, hotkeyEvent{CaptureID: s.id, Field: field, Hotkey: hk, Display: DisplayHotkey(hk)})
		return
	}
	s.m.emit(EvCaptureHotkey, hotkeyEvent{CaptureID: s.id, Field: field, Hotkey: hk, Display: DisplayHotkey(hk)})
}

func (s *sink) Key(key string) { s.KeyWithMods(key, nil) }

func (s *sink) KeyWithMods(key string, mods []string) {
	if _, ok := s.m.touch(s.gen, CaptureKey); ok {
		s.m.emit(EvCaptureKey, keyEvent{CaptureID: s.id, Key: key, Mods: mods})
	}
}

func (s *sink) Mouse(x, y int) {
	if _, ok := s.m.touch(s.gen, CaptureMouse); ok {
		s.m.emit(EvCaptureMouse, mouseEvent{CaptureID: s.id, X: x, Y: y})
	}
}
