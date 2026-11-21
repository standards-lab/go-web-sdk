# The server and the service

This page holds the reasoning behind [`Server`](../server.go), [`Service`](../service.go), and
[`webtest.Live`](../webtest/live.go).

- An unset `Config.MaxHeaderBytes` leaves `http.Server.MaxHeaderBytes` zero, net/http's signal to
  apply its own default.
- When `Shutdown`'s drain deadline passes, it closes the connections. `http.Server.Close` reaches
  only the connections net/http tracks: it neither waits for a running handler nor touches a
  hijacked connection.
- `Server` is a go-core `lifecycle.Subsystem` and `lifecycle.Monitored`, so the Coordinator
  starts, stops, and watches a Server node's value. Both of go-core's readiness sources, the
  Coordinator and `lifecycle.Readiness`, satisfy `Doctor`, so `RegisterHealth` takes either.
- `Service.Run` clips `Roots` before appending the server and lifecycle config nodes to it, so
  the appends never write into the caller's backing array.
- `webtest.Live` polls with a client of its own: a short timeout, so a service that has bound its
  port but not yet served answers false on one poll and true on a later one, and its own
  connection, so a poll never holds the suite client's one connection.
