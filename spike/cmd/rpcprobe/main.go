// rpcprobe exercises the dispatcher + DTO JSON on the build host. Its output must
// be byte-identical between a plain build and `garble -literals build`.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"spike/rpc"
	"spike/status"
)

// untaggedDTO is a deliberate negative control: no json tags.
type untaggedDTO struct {
	FieldName string
}

func main() {
	d := rpc.NewDispatcher(8)
	n := 0
	rpc.Register(d, "app.ping", func(p rpc.PingParams) (rpc.PingResult, error) {
		n++
		return rpc.PingResult{Echo: p.Text, Upper: strings.ToUpper(p.Text), Seq: n}, nil
	})
	rpc.Register(d, "app.fail", func(p rpc.PingParams) (rpc.PingResult, error) {
		return rpc.PingResult{}, &rpc.Error{Code: "CUSTOM", Message: "nope"}
	})
	out := make(chan string, 8)
	d.Start(func(b []byte) { out <- string(b) })
	defer d.Close()

	for _, raw := range []string{
		`{"id":1,"method":"app.ping","params":{"text":"héllo <b> "}}`,
		`{"id":2,"method":"app.fail"}`,
		`{"id":3,"method":"nope"}`,
		`{"id":4,"method":"app.ping","params":"bad"}`,
		`not json`,
	} {
		d.Submit(raw)
		fmt.Println(<-out)
	}

	var h status.Hub
	h.Subscribe(func(s rpc.Status) { b, _ := json.Marshal(s); fmt.Println("event", string(b)) })
	h.Publish(rpc.Status{Active: true, Running: "x", Tick: 1})
	h.Publish(rpc.Status{Active: true, Running: "x", Tick: 1})
	b, _ := json.Marshal(untaggedDTO{FieldName: "v"})
	fmt.Println("untagged", string(b))
}
