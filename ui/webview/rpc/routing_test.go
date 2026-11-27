package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func sink() (Reply, chan string) {
	ch := make(chan string, 64)
	return func(b []byte) { ch <- string(b) }, ch
}

func recv(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("no reply")
		return ""
	}
}

func quiet(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case s := <-ch:
		t.Fatalf("unexpected reply %s", s)
	case <-time.After(60 * time.Millisecond):
	}
}

var (
	main1   = Caller{Role: RoleMain, Gen: 1, HWND: 10}
	editor7 = Caller{Role: RoleEditor, Gen: 7, HWND: 20}
)

// Each request is answered to its own sink on every path; the other window never sees it.
func TestReplySinksAreRequestLocalOnEveryPath(t *testing.T) {
	d := New(map[string]Handler{
		"ok":  func(json.RawMessage) (any, error) { return 1, nil },
		"err": func(json.RawMessage) (any, error) { return nil, errors.New("x") },
	}, 8)
	d.Start()
	defer d.Close()
	mainR, mainCh := sink()
	edR, edCh := sink()
	for _, c := range []struct {
		raw  string
		code string
	}{
		{`{"id":1,"method":"ok"}`, `"ok":true`},
		{`{"id":2,"method":"err"}`, `INTERNAL`},
		{`{"id":3,"method":"nope"}`, `UNKNOWN_METHOD`},
		{`not json`, `BAD_REQUEST`},
	} {
		d.Submit(c.raw, editor7, edR)
		if got := recv(t, edCh); !contains(got, c.code) {
			t.Fatalf("%s -> %s want %s", c.raw, got, c.code)
		}
	}
	quiet(t, mainCh)
	d.Submit(`{"id":9,"method":"ok"}`, main1, mainR)
	if got := recv(t, mainCh); !contains(got, `"id":9`) {
		t.Fatal(got)
	}
	quiet(t, edCh)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBusyGoesToTheSubmitterOnly(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	d := New(map[string]Handler{"block": func(json.RawMessage) (any, error) { started <- struct{}{}; <-release; return 1, nil }}, 1)
	d.Start()
	defer d.Close()
	mainR, mainCh := sink()
	edR, edCh := sink()
	d.Submit(`{"id":1,"method":"block"}`, main1, mainR)
	<-started
	d.Submit(`{"id":2,"method":"block"}`, main1, mainR) // queued (1)
	d.Submit(`{"id":3,"method":"block"}`, editor7, edR) // queue full
	if got := recv(t, edCh); !contains(got, "BUSY") || !contains(got, `"id":3`) {
		t.Fatal(got)
	}
	quiet(t, mainCh)
	close(release)
}

func TestRoleMatrixIsDefaultDenyForEditor(t *testing.T) {
	m := NewMatrix([]string{"both"}, []string{"mainOnly"}, []string{"edOnly", "granted"}, []string{"stOnly"}).GrantMain([]string{"granted", "stOnly"})
	cases := []struct {
		role   Role
		method string
		want   bool
	}{
		{RoleMain, "both", true}, {RoleEditor, "both", true},
		{RoleMain, "mainOnly", true}, {RoleEditor, "mainOnly", false},
		{RoleMain, "edOnly", false}, {RoleEditor, "edOnly", true},
		{RoleMain, "granted", true}, {RoleEditor, "granted", true},
		{RoleMain, "stOnly", true}, {RoleEditor, "stOnly", false},
		{RoleEditor, "unlisted", false}, {RoleMain, "unlisted", true},
		{Role(0), "both", false},
	}
	for _, c := range cases {
		if got := m.Allow(c.role, c.method); got != c.want {
			t.Errorf("%v %s = %v want %v", c.role, c.method, got, c.want)
		}
	}
	if !(*Matrix)(nil).Allow(RoleEditor, "x") {
		t.Error("nil matrix must allow (no policy installed)")
	}
}

func TestForbiddenRoleRepliesToSinkWithoutRunningOrQueueing(t *testing.T) {
	var ran atomic.Int32
	d := New(map[string]Handler{"mainOnly": func(json.RawMessage) (any, error) { ran.Add(1); return 1, nil }}, 1)
	m := NewMatrix(nil, []string{"mainOnly"}, nil, nil)
	d.Authorize(func(c Caller, method string) bool { return m.Allow(c.Role, method) })
	d.Start()
	defer d.Close()
	r, ch := sink()
	for i := 0; i < 5; i++ { // more than the queue limit: denial must not consume slots
		d.Submit(`{"id":1,"method":"mainOnly"}`, editor7, r)
		if got := recv(t, ch); !contains(got, CodeForbiddenRole) {
			t.Fatal(got)
		}
	}
	d.Submit(`{"id":2,"method":"mainOnly"}`, main1, r)
	if got := recv(t, ch); !contains(got, `"ok":true`) {
		t.Fatal(got)
	}
	if ran.Load() != 1 {
		t.Fatalf("handler ran %d times", ran.Load())
	}
}

// Generation: checked at dequeue (handler not run) and at delivery (result dropped).
func TestGenerationCheckedAtDequeueAndDelivery(t *testing.T) {
	var gen atomic.Uint64
	gen.Store(7)
	var ran atomic.Int32
	enter := make(chan struct{})
	release := make(chan struct{})
	d := New(map[string]Handler{
		"slow": func(json.RawMessage) (any, error) { ran.Add(1); close(enter); <-release; return 1, nil },
		"fast": func(json.RawMessage) (any, error) { ran.Add(1); return 1, nil },
	}, 8)
	d.Validate(func(c Caller) bool { return c.Role != RoleEditor || c.Gen == gen.Load() })
	d.Start()
	defer d.Close()
	r, ch := sink()
	d.Submit(`{"id":1,"method":"slow"}`, editor7, r) // runs
	<-enter
	d.Submit(`{"id":2,"method":"fast"}`, editor7, r) // queued behind it
	gen.Store(8)                                     // window superseded while both are pending
	close(release)
	got := recv(t, ch) // only the STALE_GEN for the queued call; slow's result is dropped at delivery
	if !contains(got, CodeStaleGen) || !contains(got, `"id":2`) {
		t.Fatal(got)
	}
	quiet(t, ch)
	if ran.Load() != 1 {
		t.Fatalf("stale queued handler ran: %d", ran.Load())
	}
	d.Submit(`{"id":3,"method":"fast"}`, Caller{Role: RoleEditor, Gen: 8}, r) // the replacement works
	if got := recv(t, ch); !contains(got, `"ok":true`) {
		t.Fatal(got)
	}
}

func TestHandlerCtxSeesTheCaller(t *testing.T) {
	d := New(nil, 4)
	d.HandleCtx("who", func(ctx context.Context, _ json.RawMessage) (any, error) {
		c, ok := CallerFrom(ctx)
		return map[string]any{"ok": ok, "role": c.Role.String(), "gen": c.Gen}, nil
	})
	d.Start()
	defer d.Close()
	r, ch := sink()
	d.Submit(`{"id":1,"method":"who"}`, editor7, r)
	if got := recv(t, ch); got != `{"id":1,"ok":true,"result":{"gen":7,"ok":true,"role":"editor"}}` {
		t.Fatal(got)
	}
}

// A deferred reply is one reply, through the request's own sink, and passes the same checks.
func TestDeferredReplyOnceThroughRequestSink(t *testing.T) {
	var df atomic.Pointer[Deferred]
	d := New(map[string]Handler{"later": func(json.RawMessage) (any, error) {
		x := NewDeferred()
		df.Store(x)
		return x, nil
	}, "quick": func(json.RawMessage) (any, error) {
		x := NewDeferred()
		x.Resolve("now", nil) // resolved before the dispatcher binds
		return x, nil
	}}, 4)
	d.Start()
	defer d.Close()
	r, ch := sink()
	other, otherCh := sink()
	d.Submit(`{"id":1,"method":"later"}`, editor7, r)
	quiet(t, ch)
	// the worker is free while the reply is pending
	d.Submit(`{"id":2,"method":"quick"}`, main1, other)
	if got := recv(t, otherCh); got != `{"id":2,"ok":true,"result":"now"}` {
		t.Fatal(got)
	}
	x := df.Load()
	x.Resolve("A", nil)
	x.Resolve("B", nil) // ignored
	if got := recv(t, ch); got != `{"id":1,"ok":true,"result":"A"}` {
		t.Fatal(got)
	}
	quiet(t, ch)
	quiet(t, otherCh)
}

func TestDeferredReplyDroppedForSupersededWindowAndAfterClose(t *testing.T) {
	var gen atomic.Uint64
	gen.Store(7)
	var df atomic.Pointer[Deferred]
	d := New(map[string]Handler{"later": func(json.RawMessage) (any, error) {
		x := NewDeferred()
		df.Store(x)
		return x, nil
	}}, 4)
	d.Validate(func(c Caller) bool { return c.Gen == gen.Load() })
	d.Start()
	r, ch := sink()
	d.Submit(`{"id":1,"method":"later"}`, editor7, r)
	waitFor(t, func() bool { return df.Load() != nil })
	gen.Store(8)
	df.Load().Resolve("x", nil)
	quiet(t, ch)

	gen.Store(7)
	df.Store(nil)
	d.Submit(`{"id":2,"method":"later"}`, editor7, r)
	waitFor(t, func() bool { return df.Load() != nil })
	d.Close()
	df.Load().Resolve("x", nil)
	quiet(t, ch)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// One shared worker: two windows submitting concurrently never run handlers in parallel and each
// window's own calls keep their submission order.
func TestSingleSerialWorkerAcrossWindows(t *testing.T) {
	var inflight, maxInflight atomic.Int32
	var mu sync.Mutex
	order := map[uint64][]int{}
	d := New(map[string]Handler{"n": func(p json.RawMessage) (any, error) {
		cur := inflight.Add(1)
		for {
			m := maxInflight.Load()
			if cur <= m || maxInflight.CompareAndSwap(m, cur) {
				break
			}
		}
		var v struct {
			W uint64
			I int
		}
		_ = json.Unmarshal(p, &v)
		mu.Lock()
		order[v.W] = append(order[v.W], v.I)
		mu.Unlock()
		time.Sleep(200 * time.Microsecond)
		inflight.Add(-1)
		return v.I, nil
	}}, 1000)
	d.Start()
	defer d.Close()
	const n = 100
	var wg sync.WaitGroup
	done := make(chan struct{}, 2*n)
	for w, c := range map[uint64]Caller{1: main1, 7: editor7} {
		wg.Add(1)
		go func(w uint64, c Caller) {
			defer wg.Done()
			for i := 0; i < n; i++ {
				raw, _ := json.Marshal(map[string]any{"id": i, "method": "n", "params": map[string]any{"W": w, "I": i}})
				d.Submit(string(raw), c, func([]byte) { done <- struct{}{} })
			}
		}(w, c)
	}
	wg.Wait()
	for i := 0; i < 2*n; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d replies", i)
		}
	}
	if maxInflight.Load() != 1 {
		t.Fatalf("handlers ran concurrently: max %d", maxInflight.Load())
	}
	for w, got := range order {
		for i, v := range got {
			if v != i {
				t.Fatalf("window %d order broken at %d: %v", w, i, got[:i+1])
			}
		}
		if len(got) != n {
			t.Fatalf("window %d ran %d", w, len(got))
		}
	}
}

// Closing a window is the host's business: the dispatcher keeps serving the other window.
func TestWindowSinkClosingDoesNotCloseDispatcher(t *testing.T) {
	d := New(map[string]Handler{"ok": func(json.RawMessage) (any, error) { return 1, nil }}, 8)
	d.Start()
	defer d.Close()
	var edClosed atomic.Bool
	edR := func([]byte) {
		if edClosed.Load() {
			return // the window's own sink suppresses delivery once it is closed
		}
	}
	mainR, mainCh := sink()
	edClosed.Store(true)
	d.Submit(`{"id":1,"method":"ok"}`, editor7, edR)
	d.Submit(`{"id":2,"method":"ok"}`, main1, mainR)
	if got := recv(t, mainCh); !contains(got, `"id":2`) {
		t.Fatal(got)
	}
}
