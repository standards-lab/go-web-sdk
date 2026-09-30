// Package ratelimit is go-web-sdk's per-client HTTP rate-limiting
// middleware, over github.com/go-chi/httprate. It is a module of its own so
// an application that does not rate-limit never compiles httprate and its
// dependencies.
//
// [New] counts each client's requests in a sliding window held in memory,
// per process: two replicas each hold their own count. Its [Config] loads as
// part of an application's configuration under the block "rate_limit", with
// the RATE_LIMIT_REQUESTS and RATE_LIMIT_WINDOW overrides under the
// application's prefix.
package ratelimit
