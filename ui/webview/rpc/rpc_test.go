package rpc

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type bridgeLikeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *bridgeLikeError) Error() string { return e.Code + ": " + e.Message }

type codedErr struct{}

func (codedErr) Error() string        { return "x" }
func (codedErr) ErrorCode() string    { return "NAME_TAKEN" }
func (codedErr) ErrorMessage() string { return "taken" }

var mainCaller = Caller{Role: RoleMain, Gen: 1}

// td submits as the main window into one sink, so the single-sink tests read as before.
type td struct {
	*Dispatcher
	sink Reply
}

func (d td) Submit(raw string) { d.Dispatcher.Submit(raw, mainCaller, d.sink) }

func newStarted(t *testing.T, m map[string]Handler, q int) (td, chan string) {
	t.Helper()
	out := make(chan string, 256)
	d := New(m, q)
	d.Start()
	t.Cleanup(d.Close)
	return td{d, func(b []byte) { out <- string(b) }}, out
}

func next(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("no reply")
		return ""
	}
}

func TestDispatchResultsAndErrors(t *testing.T) {
	d, out := newStarted(t, map[string]Handler{
		"ok":     func(p json.RawMessage) (any, error) { return map[string]string{"echo": string(p)}, nil },
		"bridge": func(json.RawMessage) (any, error) { return nil, &bridgeLikeError{"HOTKEY_CONFLICT", "conflict"} },
		"coded":  func(json.RawMessage) (any, error) { return nil, codedErr{} },
		"plain":  func(json.RawMessage) (any, error) { return nil, errors.New("secret internals: C:\\path") },
		"panic":  func(json.RawMessage) (any, error) { panic("boom") },
		"bad":    func(json.RawMessage) (any, error) { return make(chan int), nil },
	}, 8)
	cases := []struct{ in, want string }{
		{`{"id":1,"method":"ok","params":{"a":1}}`, `{"id":1,"ok":true,"result":{"echo":"{\"a\":1}"}}`},
		{`{"id":2,"method":"bridge"}`, `{"id":2,"ok":false,"error":{"code":"HOTKEY_CONFLICT","message":"conflict"}}`},
		{`{"id":3,"method":"coded"}`, `{"id":3,"ok":false,"error":{"code":"NAME_TAKEN","message":"taken"}}`},
		{`{"id":4,"method":"plain"}`, `{"id":4,"ok":false,"error":{"code":"INTERNAL","message":"internal error"}}`},
		{`{"id":5,"method":"panic"}`, `{"id":5,"ok":false,"error":{"code":"INTERNAL","message":"internal error"}}`},
		{`{"id":6,"method":"nope"}`, `{"id":6,"ok":false,"error":{"code":"UNKNOWN_METHOD","message":"unknown method"}}`},
		{`{"id":7,"method":"bad"}`, `{"id":7,"ok":false,"error":{"code":"INTERNAL","message":"result not encodable"}}`},
		{`not json`, `{"id":0,"ok":false,"error":{"code":"BAD_REQUEST","message":"invalid request"}}`},
		{`{"id":9}`, `{"id":0,"ok":false,"error":{"code":"BAD_REQUEST","message":"invalid request"}}`},
	}
	for _, c := range cases {
		d.Submit(c.in)
		if got := next(t, out); got != c.want {
			t.Errorf("in %s\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

func TestSerializedSingleWorkerAndOrder(t *testing.T) {
	var mu sync.Mutex
	active, maxActive := 0, 0
	var order []int
	d, out := newStarted(t, map[string]Handler{
		"n": func(p json.RawMessage) (any, error) {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			var v int
			_ = json.Unmarshal(p, &v)
			order = append(order, v)
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
			return v, nil
		},
	}, 64)
	for i := 0; i < 40; i++ {
		d.Submit(`{"id":` + itoa(i) + `,"method":"n","params":` + itoa(i) + `}`)
	}
	for i := 0; i < 40; i++ {
		next(t, out)
	}
	if maxActive != 1 {
		t.Fatalf("calls overlapped: %d", maxActive)
	}
	for i, v := range order {
		if v != i {
			t.Fatalf("order broken at %d: %v", i, order)
		}
	}
}

func TestBusyWhenQueueFull(t *testing.T) {
	release := make(chan struct{})
	d, out := newStarted(t, map[string]Handler{
		"block": func(json.RawMessage) (any, error) { <-release; return 1, nil },
	}, 2)
	d.Submit(`{"id":1,"method":"block"}`) // taken by worker
	time.Sleep(50 * time.Millisecond)
	d.Submit(`{"id":2,"method":"block"}`)
	d.Submit(`{"id":3,"method":"block"}`) // queue now full (2)
	d.Submit(`{"id":4,"method":"block"}`)
	got := next(t, out)
	if !strings.Contains(got, `"id":4`) || !strings.Contains(got, `"code":"BUSY"`) {
		t.Fatalf("want BUSY for id 4, got %s", got)
	}
	close(release)
}

func TestCloseIsIdempotentAndSubmitAfterCloseDoesNotPanic(t *testing.T) {
	d := New(map[string]Handler{}, 0)
	d.Start()
	d.Close()
	d.Close()
	d.Submit(`{"id":1,"method":"x"}`, mainCaller, func([]byte) { t.Error("reply after Close") })
}

func TestWireFrames(t *testing.T) {
	if got := string(EncodeResolve([]byte(`{"id":1,"ok":true}`))); got != `{"k":"r","d":{"id":1,"ok":true}}` {
		t.Fatal(got)
	}
	b, err := EncodeEvent("status", map[string]any{"a": "</script>\u2028"})
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		K string
		N string
		P map[string]string
	}
	if err := json.Unmarshal(b, &m); err != nil || m.K != "e" || m.N != "status" || m.P["a"] != "</script>\u2028" {
		t.Fatalf("%s %v", b, err)
	}
	if strings.Contains(string(b), "</script>") {
		t.Fatal("payload must be html-escaped by json.Marshal")
	}
	if _, err := EncodeEvent("x", make(chan int)); err == nil {
		t.Fatal("unencodable payload must error")
	}
	for _, s := range []string{"addEventListener('message'", "__resolve", "__emit"} {
		if !strings.Contains(BootstrapJS, s) {
			t.Errorf("bootstrap lacks %s", s)
		}
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

// session models the bridge's editor session: editor.open opens it, the reset job closes it.
type session struct {
	mu    sync.Mutex
	open  bool
	trace []string
}

func (s *session) mark(ev string, open bool) {
	s.mu.Lock()
	s.open = open
	s.trace = append(s.trace, ev)
	s.mu.Unlock()
}

func TestResetJobRunsAfterQueuedOpenAndLeavesNoSession(t *testing.T) {
	release := make(chan struct{})
	s := &session{}
	d, out := newStarted(t, map[string]Handler{
		"block":       func(json.RawMessage) (any, error) { <-release; s.mark("block", s.open); return 1, nil },
		"editor.open": func(json.RawMessage) (any, error) { s.mark("open", true); return "session", nil },
	}, 8)
	d.Submit(`{"id":1,"method":"block"}`) // in flight
	time.Sleep(30 * time.Millisecond)
	d.Submit(`{"id":2,"method":"editor.open"}`) // queued by the crashed page
	jobDone := make(chan bool, 1)
	if !d.Enqueue(func() { s.mark("reset", false); jobDone <- true }) {
		t.Fatal("job refused")
	}
	close(release)
	next(t, out)
	next(t, out)
	select {
	case <-jobDone:
	case <-time.After(2 * time.Second):
		t.Fatal("reset job never ran")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open {
		t.Fatal("an editor.open queued before the reset job left a session open")
	}
	if got := strings.Join(s.trace, ","); got != "block,open,reset" {
		t.Fatalf("order %s", got)
	}
}

func TestCallSubmittedAfterJobRunsAfterIt(t *testing.T) {
	release := make(chan struct{})
	s := &session{}
	d, out := newStarted(t, map[string]Handler{
		"app.getBootState": func(json.RawMessage) (any, error) { s.mark("boot", s.open); return "boot", nil },
	}, 8)
	d.Enqueue(func() { <-release; s.mark("reset", false) }) // slow reset
	d.Submit(`{"id":1,"method":"app.getBootState"}`)        // new document's boot call
	select {
	case r := <-out:
		t.Fatalf("call overtook the reset job: %s", r)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if r := next(t, out); r != `{"id":1,"ok":true,"result":"boot"}` {
		t.Fatalf("boot call must succeed after the job: %s", r)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if got := strings.Join(s.trace, ","); got != "reset,boot" {
		t.Fatalf("order %s", got)
	}
}

func TestJobIsNeverRefusedForFullness(t *testing.T) {
	release := make(chan struct{})
	ran := make(chan struct{})
	d, out := newStarted(t, map[string]Handler{"block": func(json.RawMessage) (any, error) { <-release; return 1, nil }}, 1)
	d.Submit(`{"id":1,"method":"block"}`)
	time.Sleep(30 * time.Millisecond)
	d.Submit(`{"id":2,"method":"block"}`) // fills the request limit
	if !d.Enqueue(func() { close(ran) }) {
		t.Fatal("a reset job must not be refused when the request queue is full")
	}
	d.Submit(`{"id":3,"method":"block"}`)
	if r := next(t, out); !strings.Contains(r, `"id":3`) || !strings.Contains(r, CodeBusy) {
		t.Fatalf("request over the limit: %s", r)
	}
	close(release)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not run")
	}
}

func TestCloseIsTerminalQuarantine(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	var ran []string
	rec := func(n string) { mu.Lock(); ran = append(ran, n); mu.Unlock() }
	d, out := newStarted(t, map[string]Handler{
		"block": func(json.RawMessage) (any, error) { <-release; rec("block"); return 1, nil },
		"x":     func(json.RawMessage) (any, error) { rec("x"); return 1, nil },
	}, 8)
	d.Submit(`{"id":1,"method":"block"}`) // in flight
	time.Sleep(30 * time.Millisecond)
	d.Submit(`{"id":2,"method":"x"}`) // queued: must be discarded
	d.Enqueue(func() { rec("job") })  // queued: must be discarded
	d.Close()
	if d.Enqueue(func() { rec("late job") }) {
		t.Fatal("Enqueue after Close must report false")
	}
	d.Submit(`{"id":3,"method":"x"}`)
	d.Submit(`not json`)
	close(release)
	if !d.Wait(2 * time.Second) {
		t.Fatal("worker did not stop after the call in flight")
	}
	select {
	case r := <-out:
		t.Fatalf("no reply may be delivered after Close: %s", r)
	case <-time.After(50 * time.Millisecond):
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(ran, ","); got != "block" {
		t.Fatalf("only the in-flight call may finish: %s", got)
	}
}
