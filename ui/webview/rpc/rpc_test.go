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

func newStarted(t *testing.T, m map[string]Handler, q int) (*Dispatcher, chan string) {
	t.Helper()
	out := make(chan string, 256)
	d := New(m, q)
	d.Start(func(b []byte) { out <- string(b) })
	t.Cleanup(d.Close)
	return d, out
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
	d.Start(func([]byte) {})
	d.Close()
	d.Close()
	d.Submit(`{"id":1,"method":"x"}`)
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

func TestHoldQueuesNewCallsAndRunsThemAfterRelease(t *testing.T) {
	d, out := newStarted(t, map[string]Handler{"app.getBootState": func(json.RawMessage) (any, error) { return "boot", nil }}, 4)
	d.BeginDocument()
	d.BeginDocument() // nests
	d.Submit(`{"id":1,"method":"app.getBootState"}`)
	select {
	case s := <-out:
		t.Fatalf("held call must be queued, not answered: %s", s)
	case <-time.After(50 * time.Millisecond):
	}
	d.Release()
	select {
	case s := <-out:
		t.Fatalf("still held once: %s", s)
	case <-time.After(50 * time.Millisecond):
	}
	d.Release()
	if s := next(t, out); s != `{"id":1,"ok":true,"result":"boot"}` {
		t.Fatalf("new-document boot call must succeed after Release: %s", s)
	}
}

func TestOldGenerationQueuedCallNeverRunsAfterReset(t *testing.T) {
	release := make(chan struct{})
	opened := make(chan struct{}, 1)
	d, out := newStarted(t, map[string]Handler{
		"block":       func(json.RawMessage) (any, error) { <-release; return 1, nil },
		"editor.open": func(json.RawMessage) (any, error) { opened <- struct{}{}; return "session", nil },
		"boot":        func(json.RawMessage) (any, error) { return "ok", nil },
	}, 8)
	d.Submit(`{"id":1,"method":"block"}`) // running in the worker
	time.Sleep(50 * time.Millisecond)
	d.Submit(`{"id":2,"method":"editor.open"}`) // queued, belongs to the OLD document
	d.BeginDocument()                           // navigation starts
	d.Submit(`{"id":3,"method":"boot"}`)        // NEW document, queued by the hold
	close(release)
	if s := next(t, out); s != `{"id":1,"ok":true,"result":1}` {
		t.Fatalf("in-flight call finishes: %s", s)
	}
	if s := next(t, out); !strings.Contains(s, `"id":2`) || !strings.Contains(s, `"code":"STALE_PAGE"`) {
		t.Fatalf("old-generation call must be dropped: %s", s)
	}
	d.Release() // reset done
	if s := next(t, out); s != `{"id":3,"ok":true,"result":"ok"}` {
		t.Fatalf("new document call: %s", s)
	}
	select {
	case <-opened:
		t.Fatal("the old document's editor.open ran after the reset")
	default:
	}
}

func TestHoldOverflowAnswersBusy(t *testing.T) {
	d, out := newStarted(t, map[string]Handler{"x": func(json.RawMessage) (any, error) { return 1, nil }}, 1)
	d.Hold()
	d.Submit(`{"id":1,"method":"x"}`)
	d.Submit(`{"id":2,"method":"x"}`)
	if s := next(t, out); !strings.Contains(s, `"id":2`) || !strings.Contains(s, `"code":"BUSY"`) {
		t.Fatalf("overflow: %s", s)
	}
}

func TestRunWithinDeadline(t *testing.T) {
	if !RunWithin(time.Second, func() {}) {
		t.Fatal("a fast reset must complete")
	}
	block := make(chan struct{})
	defer close(block)
	start := time.Now()
	if RunWithin(80*time.Millisecond, func() { <-block }) {
		t.Fatal("a stuck reset must report the deadline (the caller takes the fatal path)")
	}
	if time.Since(start) > time.Second {
		t.Fatal("deadline not enforced")
	}
}
