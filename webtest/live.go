package webtest

import (
	"net/http"
	"time"

	"github.com/standards-lab/go-web-sdk"
)

var probe = newHTTPClient(time.Second)

// Live reports whether the service at base answers its liveness probe with
// 200. A server node that uses what it serves sits in a layer above those
// nodes, and the Coordinator starts a layer only once every layer beneath it
// has started, so a live probe means they all have; a harness passes it as
// the condition to processtest's Await.
func Live(base string) bool {
	res, err := probe.Get(base + web.HealthPath)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK
}
