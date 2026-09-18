package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/httprate"
	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk"
)

const (
	defaultRequests = 300
	defaultWindow   = time.Minute
)

// Env names the environment variables [Config.FinalizeBlock] reads, composed
// from the prefix and block it receives: <BLOCK>_REQUESTS and <BLOCK>_WINDOW
// under whatever prefix [config.EnvName] produces. An empty name disables
// that one override, and the zero Env (an empty prefix) disables both.
// Populated by FinalizeBlock and exposed for introspection.
type Env struct {
	Requests string
	Window   string
}

// NewEnv composes the override names from a prefix and a block segment:
// <BLOCK>_REQUESTS and <BLOCK>_WINDOW under whatever prefix
// [config.EnvName] produces. The block distinguishes one Config from another
// under the same prefix, so a second limiter (block "login", say) composes
// names that do not collide with the primary limit's (block "rate_limit").
// An empty prefix returns the zero Env, disabling the overrides.
func NewEnv(prefix, block string) Env {
	if prefix == "" {
		return Env{}
	}
	return Env{
		Requests: config.EnvName(
			prefix, block, "requests",
		),
		Window: config.EnvName(
			prefix, block, "window",
		),
	}
}

// Config holds the limit: Requests is how many requests one client may make
// per Window. Both are tri-state pointers: nil is unset and takes the
// default (300 requests per minute), and a set value survives the load. Env
// records the environment-variable names Finalize composed and read; it is
// excluded from JSON.
type Config struct {
	Requests *int             `json:"requests"`
	Window   *config.Duration `json:"window"`
	Env      Env              `json:"-"`
}

// Merge overlays src's set fields onto the receiver.
func (c *Config) Merge(src *Config) {
	if src.Requests != nil {
		c.Requests = src.Requests
	}
	if src.Window != nil {
		c.Window = src.Window
	}
}

// Finalize finalizes the receiver as the primary limit: it delegates to
// [Config.FinalizeBlock] with the block "rate_limit", so the override names
// it composes are RATE_LIMIT_REQUESTS and RATE_LIMIT_WINDOW under envPrefix.
// This is the method [config.Load] calls.
func (c *Config) Finalize(envPrefix string) error {
	return c.FinalizeBlock(envPrefix, "rate_limit")
}

// FinalizeBlock composes the environment override names from envPrefix and
// block (an empty prefix disables overrides), applies defaults, applies the
// overrides, and validates. The block segment lets a second Config (a
// tighter limit on a login route, say) finalize under the same prefix as
// the primary limit without their override names colliding.
func (c *Config) FinalizeBlock(envPrefix, block string) error {
	c.Env = NewEnv(envPrefix, block)
	c.applyDefaults()
	if err := c.applyEnv(); err != nil {
		return err
	}
	return c.validate()
}

func (c *Config) applyDefaults() {
	if c.Requests == nil {
		c.Requests = new(defaultRequests)
	}
	if c.Window == nil {
		c.Window = new(config.Duration(defaultWindow))
	}
}

func (c *Config) applyEnv() error {
	if v := os.Getenv(c.Env.Requests); v != "" {
		requests, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Env.Requests, err)
		}
		c.Requests = &requests
	}
	return config.SetDurationFromEnv(&c.Window, c.Env.Window)
}

// validate rejects a non-positive request count and a window under one
// second: httprate reports the window to the client in whole seconds
// (Retry-After, X-RateLimit-Reset), so a shorter one cannot be expressed
// there.
func (c *Config) validate() error {
	if *c.Requests <= 0 {
		return fmt.Errorf("invalid requests: %d", *c.Requests)
	}
	if *c.Window < config.Duration(time.Second) {
		return fmt.Errorf("invalid window: %s", c.Window)
	}
	return nil
}

func (c *Config) finalized() bool {
	return c.Requests != nil && c.Window != nil
}

// New limits each client to cfg.Requests requests per cfg.Window, keyed by
// the request's remote address: the host part of r.RemoteAddr, with an IPv6
// address reduced to its /64 through [httprate.CanonicalizeIP] so a client
// cannot rotate through its own block to win a fresh bucket per request.
// The count is a sliding window held in memory, per process; httprate sets
// the X-RateLimit-Limit, X-RateLimit-Remaining, and X-RateLimit-Reset
// headers on every response it judges.
//
// A request over the limit is answered with a 429 problem document through
// [web.WriteProblem] and never reaches the next handler. httprate sets the
// Retry-After header, the window in whole seconds, before the document is
// written, so it goes out with the response. The 429 is otherwise
// undecorated (about:blank, the status phrase as title, the request path as
// instance); its detail names the configured limit. The encoder's error is
// dropped, matching the base module's other direct-write middleware; nothing
// here logs.
//
// A failure inside httprate itself (the key function, or the counter) is
// answered with a 500 problem document whose detail is a fixed message; the
// error text stays off the wire. Neither the key function here nor the
// in-memory counter can fail, so that path is wired for a future counter
// that could, not for anything in this build.
//
// New panics on a Config that has not been finalized (a nil Requests or
// Window) or that would not validate: a struct built by hand that skipped
// [Config.Finalize] is a wiring mistake, and a middleware that quietly
// applied a zero limit would refuse every request it saw.
func New(cfg Config) web.Middleware {
	if !cfg.finalized() {
		panic("ratelimit: New requires a finalized Config; call Finalize first")
	}
	if err := cfg.validate(); err != nil {
		panic("ratelimit: New requires a valid Config: " + err.Error())
	}

	detail := fmt.Sprintf(
		"The client exceeded the limit of %d requests per %s.",
		*cfg.Requests, cfg.Window,
	)
	limited := func(w http.ResponseWriter, r *http.Request) {
		_ = web.WriteProblem(w, r, http.StatusTooManyRequests, "", detail)
	}

	return httprate.LimitBy(*cfg.Requests, cfg.Window.Duration(), keyByRemoteAddr,
		httprate.WithLimitHandler(limited), httprate.WithErrorHandler(failed))
}

// keyByRemoteAddr keys a request by the host part of its remote address,
// canonicalized through [httprate.CanonicalizeIP]. A RemoteAddr with no port
// (some test setups) is used as it is. The error return is httprate's
// signature; nothing here can fail.
func keyByRemoteAddr(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return httprate.CanonicalizeIP(host), nil
}

// failed answers httprate's own error path with a 500 problem document. The
// error stays out of the document; the detail is a fixed message.
func failed(w http.ResponseWriter, r *http.Request, _ error) {
	_ = web.WriteProblem(
		w,
		r,
		http.StatusInternalServerError,
		"",
		"The server could not evaluate the rate limit.",
	)
}
