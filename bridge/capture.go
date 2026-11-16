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
type CaptureManager struct {
	backend CaptureBackend
	emit    func(event string, payload any)
	idle    time.Duration

	mu       sync.Mutex
	gen      uint64
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

// Active reports the current mode and session.
func (m *CaptureManager) Active() (CaptureMode, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mode, m.session
}

// Start begins a capture, ending any previous one with reason "stopped".
func (m *CaptureManager) Start(session string, mode CaptureMode, field string, pickKey model.Hotkey) error {
	m.End(EndStopped)
	if m.backend == nil {
		return newErr(CodeInternal)
	}
	m.mu.Lock()
	m.gen++
	gen := m.gen
	m.mode, m.session, m.field = mode, session, field
	m.armLocked(gen)
	m.mu.Unlock()
	if err := m.backend.Start(mode, pickKey, &sink{m: m, gen: gen}); err != nil {
		m.endGen(gen, "", false)
		return newErr(CodeInternal)
	}
	return nil
}

// End stops the active capture (if any), removes the hook and emits capture.ended{reason}.
func (m *CaptureManager) End(reason string) { m.endGen(0, reason, true) }

func (m *CaptureManager) endGen(gen uint64, reason string, notify bool) {
	m.mu.Lock()
	if m.mode == CaptureNone || (gen != 0 && m.gen != gen) {
		m.mu.Unlock()
		return
	}
	m.gen++
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
		m.emit(EvCaptureEnded, map[string]string{"reason": reason})
	}
}

// armLocked (re)starts the single idle timer; mu held.
func (m *CaptureManager) armLocked(gen uint64) {
	m.deadline = time.Now().Add(m.idle)
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(m.idle, func() { m.endGen(gen, EndTimeout, true) })
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
}

func (s *sink) Hotkey(hk model.Hotkey) {
	field, ok := s.m.touch(s.gen, CaptureHotkey)
	if !ok {
		return
	}
	s.m.emit(EvCaptureHotkey, map[string]any{"field": field, "hotkey": hk, "display": DisplayHotkey(hk)})
}

func (s *sink) Key(key string) {
	if _, ok := s.m.touch(s.gen, CaptureKey); ok {
		s.m.emit(EvCaptureKey, map[string]string{"key": key})
	}
}

func (s *sink) Mouse(x, y int) {
	if _, ok := s.m.touch(s.gen, CaptureMouse); ok {
		s.m.emit(EvCaptureMouse, map[string]int{"x": x, "y": y})
	}
}
