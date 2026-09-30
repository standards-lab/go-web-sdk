// Package webtest is the integration toolkit for a service built on the web
// package. It provides the client a black-box suite drives the running
// service through, which reads responses and RFC 9457 problems the way the
// web package writes them, and the liveness observation a harness waits on.
// It is the HTTP half of the toolkit go-core's processtest package begins: a
// harness launches the binary with processtest and observes it with webtest.
// This comment lists every exported name; each symbol's own documentation
// states its contract.
//
// [Client] issues requests against one service and returns each [Response]
// whole, so a test asserts on status, headers, and body without a transport
// in view.
//
//   - [NewClient] binds a [Client] to a service's URL. [Client.Do] sends any
//     method, and [Client.Get], [Client.Post], [Client.Put], and
//     [Client.Delete] each send the method they are named for.
//   - [Header] is one request header, and [IfMatch] is the precondition
//     header a guarded command takes.
//   - [Raw] is a body sent under its own media type, such as a file's bytes
//     for an upload.
//   - [Response.Expect] asserts a status, [Response.JSON] decodes the body,
//     [Decode] does both for the common read, and [Response.Problem] asserts
//     a problem document.
//   - [Live] reports whether a service answers its liveness probe, the
//     condition a harness passes to processtest's Await.
package webtest
