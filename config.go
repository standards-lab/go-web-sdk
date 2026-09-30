package web

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/standards-lab/go-core/config"
)

const (
	defaultHost              = "0.0.0.0"
	defaultPort              = 8080
	defaultReadTimeout       = 30 * time.Second
	defaultReadHeaderTimeout = 5 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 2 * time.Minute
	defaultTransferRate      = 64 << 10
)

// Env records the environment-variable names [Config.FinalizeBlock]
// composed from its prefix and block: <BLOCK>_HOST, <BLOCK>_PORT, the
// four <BLOCK>_*_TIMEOUT names, and <BLOCK>_TRANSFER_RATE under the
// prefix. An empty prefix composes none, disabling the overrides.
type Env struct {
	Host              string
	Port              string
	ReadTimeout       string
	ReadHeaderTimeout string
	WriteTimeout      string
	IdleTimeout       string
	TransferRate      string
}

func newEnv(prefix, block string) Env {
	return Env{
		Host:              config.EnvName(prefix, block, "host"),
		Port:              config.EnvName(prefix, block, "port"),
		ReadTimeout:       config.EnvName(prefix, block, "read", "timeout"),
		ReadHeaderTimeout: config.EnvName(prefix, block, "read", "header", "timeout"),
		WriteTimeout:      config.EnvName(prefix, block, "write", "timeout"),
		IdleTimeout:       config.EnvName(prefix, block, "idle", "timeout"),
		TransferRate:      config.EnvName(prefix, block, "transfer", "rate"),
	}
}

// Config holds the server's address, timeouts, header limit, and transfer
// rate, loaded through go-core's config contract. The pointer fields are
// unset when nil and take the default at Finalize, while an explicit zero
// survives: a disabled timeout, or an ephemeral port. MaxHeaderBytes has no
// default of its own; unset, net/http applies [http.DefaultMaxHeaderBytes].
//
// The read and write timeouts default to 30 seconds each, and
// TransferRate, in bytes per second, to 64 KiB/s. [Transfer] describes how
// a route combines them.
type Config struct {
	Host              string           `json:"host"`
	Port              *int             `json:"port"`
	ReadTimeout       *config.Duration `json:"read_timeout"`
	ReadHeaderTimeout *config.Duration `json:"read_header_timeout"`
	WriteTimeout      *config.Duration `json:"write_timeout"`
	IdleTimeout       *config.Duration `json:"idle_timeout"`
	MaxHeaderBytes    *int             `json:"max_header_bytes"`
	TransferRate      *int64           `json:"transfer_rate"`
	Env               Env              `json:"-"`
}

// Addr joins the host and port; an unset port reads as 0.
func (c *Config) Addr() string {
	port := 0
	if c.Port != nil {
		port = *c.Port
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

// Merge overlays src's set fields onto the receiver.
func (c *Config) Merge(src *Config) {
	if src.Host != "" {
		c.Host = src.Host
	}
	if src.Port != nil {
		c.Port = src.Port
	}
	if src.ReadTimeout != nil {
		c.ReadTimeout = src.ReadTimeout
	}
	if src.ReadHeaderTimeout != nil {
		c.ReadHeaderTimeout = src.ReadHeaderTimeout
	}
	if src.WriteTimeout != nil {
		c.WriteTimeout = src.WriteTimeout
	}
	if src.IdleTimeout != nil {
		c.IdleTimeout = src.IdleTimeout
	}
	if src.MaxHeaderBytes != nil {
		c.MaxHeaderBytes = src.MaxHeaderBytes
	}
	if src.TransferRate != nil {
		c.TransferRate = src.TransferRate
	}
}

// Finalize is [Config.FinalizeBlock] under the block "server", the method
// [config.Load] calls.
func (c *Config) Finalize(envPrefix string) error {
	return c.FinalizeBlock(envPrefix, "server")
}

// FinalizeBlock composes [Env] from envPrefix and block, applies the
// defaults, then the environment overrides, and validates. A second Config,
// such as a management listener's, finalizes under its own block so its
// override names do not collide with the primary server's.
func (c *Config) FinalizeBlock(envPrefix, block string) error {
	c.Env = newEnv(envPrefix, block)
	c.applyDefaults()
	if err := c.applyEnv(); err != nil {
		return err
	}
	return c.validate()
}

func (c *Config) applyDefaults() {
	if c.Host == "" {
		c.Host = defaultHost
	}
	if c.Port == nil {
		c.Port = new(defaultPort)
	}
	if c.ReadTimeout == nil {
		c.ReadTimeout = new(config.Duration(defaultReadTimeout))
	}
	if c.ReadHeaderTimeout == nil {
		c.ReadHeaderTimeout = new(config.Duration(defaultReadHeaderTimeout))
	}
	if c.WriteTimeout == nil {
		c.WriteTimeout = new(config.Duration(defaultWriteTimeout))
	}
	if c.IdleTimeout == nil {
		c.IdleTimeout = new(config.Duration(defaultIdleTimeout))
	}
	if c.TransferRate == nil {
		c.TransferRate = new(int64(defaultTransferRate))
	}
}

func (c *Config) applyEnv() error {
	if v := os.Getenv(c.Env.Host); v != "" {
		c.Host = v
	}
	if err := config.SetFromEnv(&c.Port, c.Env.Port, strconv.Atoi); err != nil {
		return err
	}
	if err := config.SetDurationFromEnv(&c.ReadTimeout, c.Env.ReadTimeout); err != nil {
		return err
	}
	if err := config.SetDurationFromEnv(&c.ReadHeaderTimeout, c.Env.ReadHeaderTimeout); err != nil {
		return err
	}
	if err := config.SetDurationFromEnv(&c.WriteTimeout, c.Env.WriteTimeout); err != nil {
		return err
	}
	if err := config.SetDurationFromEnv(&c.IdleTimeout, c.Env.IdleTimeout); err != nil {
		return err
	}
	return config.SetFromEnv(&c.TransferRate, c.Env.TransferRate, func(s string) (int64, error) {
		return strconv.ParseInt(s, 10, 64)
	})
}

func (c *Config) validate() error {
	if *c.Port < 0 || *c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", *c.Port)
	}
	if *c.ReadTimeout < 0 {
		return fmt.Errorf("invalid read_timeout: %s", c.ReadTimeout)
	}
	if *c.ReadHeaderTimeout < 0 {
		return fmt.Errorf("invalid read_header_timeout: %s", c.ReadHeaderTimeout)
	}
	if *c.WriteTimeout < 0 {
		return fmt.Errorf("invalid write_timeout: %s", c.WriteTimeout)
	}
	if *c.IdleTimeout < 0 {
		return fmt.Errorf("invalid idle_timeout: %s", c.IdleTimeout)
	}
	if c.MaxHeaderBytes != nil && *c.MaxHeaderBytes < 0 {
		return fmt.Errorf("invalid max_header_bytes: %d", *c.MaxHeaderBytes)
	}
	if *c.TransferRate <= 0 {
		return fmt.Errorf("invalid transfer_rate: %d", *c.TransferRate)
	}
	return nil
}

func (c *Config) finalized() bool {
	return c.Port != nil &&
		c.ReadTimeout != nil &&
		c.ReadHeaderTimeout != nil &&
		c.WriteTimeout != nil &&
		c.IdleTimeout != nil &&
		c.TransferRate != nil
}
