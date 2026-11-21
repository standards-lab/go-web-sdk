package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/httprate"
	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk"
)

// See docs/middleware.md

const (
	defaultRequests = 300
	defaultWindow   = time.Minute
)

// Env records the environment-variable names [Config.FinalizeBlock]
// composed from its prefix and block: <BLOCK>_REQUESTS and <BLOCK>_WINDOW
// under the prefix. An empty prefix composes none, disabling the overrides.
type Env struct {
	Requests string
	Window   string
}

func newEnv(prefix, block string) Env {
	return Env{
		Requests: config.EnvName(prefix, block, "requests"),
		Window:   config.EnvName(prefix, block, "window"),
	}
}

// Config holds the limit, Requests per Window, loaded through go-core's
// config contract; nil fields take the default, 300 requests per minute.
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

// Finalize is [Config.FinalizeBlock] under the block "rate_limit", the
// method [config.Load] calls.
func (c *Config) Finalize(envPrefix string) error {
	return c.FinalizeBlock(envPrefix, "rate_limit")
}

// FinalizeBlock composes [Env] from envPrefix and block, applies the
// defaults, then the environment overrides, and validates. A second limit,
// such as a tighter one on a login route, finalizes under its own block.
func (c *Config) FinalizeBlock(envPrefix, block string) error {
	c.Env = newEnv(envPrefix, block)
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
	if err := config.SetFromEnv(&c.Requests, c.Env.Requests, strconv.Atoi); err != nil {
		return err
	}
	return config.SetDurationFromEnv(&c.Window, c.Env.Window)
}

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

// New limits each client to cfg.Requests per cfg.Window, keying a client by
// its remote address with IPv6 reduced to its /64 and an IPv4-mapped IPv6
// address keyed as its IPv4 address. It answers a request over
// the limit with a 429 problem and Retry-After, the window in whole seconds.
// Every response it judges carries X-RateLimit-Limit, X-RateLimit-Remaining,
// and X-RateLimit-Reset. It panics on a Config that has not passed Finalize.
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
		_ = web.Problem{Status: http.StatusTooManyRequests, Detail: detail}.WriteFor(w, r)
	}

	return httprate.LimitBy(*cfg.Requests, cfg.Window.Duration(), keyByRemoteAddr,
		httprate.WithLimitHandler(limited), httprate.WithErrorHandler(failed))
}

func keyByRemoteAddr(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return httprate.CanonicalizeIP(host), nil
}

func failed(w http.ResponseWriter, r *http.Request, _ error) {
	_ = web.Problem{
		Status: http.StatusInternalServerError,
		Detail: "The server could not evaluate the rate limit.",
	}.WriteFor(w, r)
}
