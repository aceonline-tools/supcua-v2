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
)

// Capture end reasons (contract §9.5).
const (
	EndStopped      = "stopped"
	EndBlur         = "blur"
	EndTimeout      = "timeout"
	EndEditorClosed = "editorClosed"
)

const defaultCaptureIdle = 60 * time.Second

// CaptureSink receives backend events. Methods may be called from any goroutine; events from a
// stopped session are dropped by the manager.
type CaptureSink interface {
	Hotkey(hk model.Hotkey)
	Key(key string)
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
	endedID := m.id
	m.mode, m.session, m.field = CaptureNone, "", ""
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	if m.backend != nil {
		m.backend.Stop()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen != gen || m.mode == CaptureNone || (want != CaptureNone && m.mode != want) {
		return "", false
	}
	m.armLocked(gen)
	return m.field, true
}

type sink struct {
	m   *CaptureManager
	gen uint64
	id  uint64
}

func (s *sink) Hotkey(hk model.Hotkey) {
	field, ok := s.m.touch(s.gen, CaptureHotkey)
	if !ok {
		return
	}
	s.m.emit(EvCaptureHotkey, hotkeyEvent{CaptureID: s.id, Field: field, Hotkey: hk, Display: DisplayHotkey(hk)})
}

func (s *sink) Key(key string) {
	if _, ok := s.m.touch(s.gen, CaptureKey); ok {
		s.m.emit(EvCaptureKey, keyEvent{CaptureID: s.id, Key: key})
	}
}

func (s *sink) Mouse(x, y int) {
	if _, ok := s.m.touch(s.gen, CaptureMouse); ok {
		s.m.emit(EvCaptureMouse, mouseEvent{CaptureID: s.id, X: x, Y: y})
	}
}
