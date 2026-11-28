# The server and the service

This page holds the reasoning behind [`Server`](../server.go), [`Service`](../service.go), and
[`webtest.Live`](../webtest/live.go).

- An unset `Config.MaxHeaderBytes` leaves `http.Server.MaxHeaderBytes` zero, net/http's signal to
  apply its own default.
- When `Shutdown`'s drain deadline passes, it closes the connections. `http.Server.Close` reaches
  only the connections net/http tracks: it neither waits for a running handler nor touches a
  hijacked connection.
- `Start` keeps its context, the Coordinator's run context, and `http.Server.BaseContext` carries
  it into every request's context as a value under an unexported key, not as the request
  context's parent. The Coordinator cancels the run context before it calls `Shutdown`, and
  `http.Server.Shutdown` waits for every active connection. A long-lived response such as a
  server-sent event stream would hold its connection past the drain deadline, so the stream writer
  reads the carried run context and ends when it does
  ([requests and responses](requests-and-responses.md#the-event-stream)). Were the run context the parent, every
  request would be cancelled at the start of the drain, and an ordinary request, such as an
  upload's PATCH, could no longer drain to completion. A request served by a handler without a
  `Server`, as under a plain `httptest` server, carries no run context, and `EventStream` treats
  its absence as a run that never ends.
- `Server` is a go-core `lifecycle.Subsystem` and `lifecycle.Monitored`, so the Coordinator
  starts, stops, and watches a Server node's value. Both of go-core's readiness sources, the
  Coordinator and `lifecycle.Readiness`, satisfy `Doctor`, so `RegisterHealth` takes either.
- `Service.Run` clips `Roots` before appending the server and lifecycle config nodes to it, so
  the appends never write into the caller's backing array.
- `webtest.Live` polls with a client of its own: a short timeout, so a service that has bound its
  port but not yet served answers false on one poll and true on a later one, and its own
  connection, so a poll never holds the suite client's one connection.
