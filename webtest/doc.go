// Package webtest is the integration toolkit for a service built on the web
// package: the client a black-box suite drives the running service through,
// reading responses and RFC 9457 problems the way the package writes them,
// and the liveness observation a harness waits on. It is the HTTP half of
// the toolkit go-core's processtest package begins: a harness launches the
// binary there and observes it here.
//
// [Client] issues requests against one service and returns each [Response]
// whole, so a test asserts on status, headers, and body without a transport
// in view; [Response.Problem] asserts a problem document, [Decode] the
// common read. [IfMatch] is the precondition header a guarded command takes.
// [Live] reports whether a service answers its liveness probe, the condition
// a harness passes to processtest's Await.
//
// [Probe] is the unit-tier helper: one request served through a handler
// into a recorder, for a handler test that needs no server.
package webtest
