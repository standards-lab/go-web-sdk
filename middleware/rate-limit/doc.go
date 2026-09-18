// Package ratelimit is go-web-sdk's per-key HTTP rate-limiting middleware,
// over github.com/go-chi/httprate. It is a separate module from the base
// go-web-sdk module because httprate and its indirect dependencies (an xxh3
// hash and a CPU-feature probe beneath it) are weight most consumers of web
// should not compile to get the server and the router: an application that
// does not rate-limit never pulls them.
//
// [New] returns a [web.Middleware] that counts requests per client in a
// sliding window held in memory, keyed by the host part of the request's
// remote address with an IPv6 address reduced to its /64, and refuses a
// request over the limit with a 429 RFC 9457 problem document and a
// Retry-After header. The count is per process: two replicas each hold
// their own.
//
// [Config] holds the limit, Requests per Window, and implements the Merge
// and Finalize contract of go-core's config package, so it loads as part of
// an application's configuration under the block "rate_limit", with the
// RATE_LIMIT_REQUESTS and RATE_LIMIT_WINDOW environment overrides under the
// application's prefix. Both fields are pointers: nil is unset and takes
// the default, 300 requests per minute. [Config.FinalizeBlock] finalizes
// under a caller-named block instead, so a second limit (a tighter one on a
// login route, say) loads under the same prefix without its override names
// colliding with the primary limit's. New panics on a Config that was not
// finalized, or that would not validate, as a wiring mistake.
package ratelimit
