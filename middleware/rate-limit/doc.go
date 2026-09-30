// Package ratelimit is go-web-sdk's per-client HTTP rate-limiting
// middleware, over github.com/go-chi/httprate. It is a module of its own so
// an application that does not rate-limit never compiles httprate and its
// dependencies. This comment places every exported name; each symbol's own
// documentation states its contract.
//
// [New] counts each client's requests in a sliding window held in memory,
// per process: two replicas each hold their own count. Its [Config] loads as
// part of an application's configuration under the block "rate_limit", with
// the RATE_LIMIT_REQUESTS and RATE_LIMIT_WINDOW overrides under the
// application's prefix.
//
//   - [New] returns the [web.Middleware] that limits each client and
//     answers a request over the limit with a 429 problem.
//   - [Config] is the limit, Requests per Window, with nil fields taking the
//     default at [Config.Finalize]; [Config.FinalizeBlock] loads a second
//     limit under a block of its own.
//   - [Env] records the override names Finalize composed.
package ratelimit
