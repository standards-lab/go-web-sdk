package web

import "github.com/standards-lab/go-core/config"

// Env names the environment variables [Config.Finalize] reads, composed from
// the prefix it receives: SERVER_HOST, SERVER_PORT, and the four timeout
// names under whatever prefix [config.EnvName] produces. An empty name
// disables that one override; the zero value — an empty prefix — disables all
// of them. Populated by Finalize and exposed for introspection.
type Env struct {
	Host              string
	Port              string
	ReadTimeout       string
	ReadHeaderTimeout string
	WriteTimeout      string
	IdleTimeout       string
}

// NewEnv composes the standard override names from a prefix: SERVER_HOST,
// SERVER_PORT, and the four timeout names under whatever prefix
// [config.EnvName] produces. An empty prefix returns the zero Env, disabling
// the overrides.
func NewEnv(prefix string) Env {
	if prefix == "" {
		return Env{}
	}
	return Env{
		Host: config.EnvName(
			prefix, "server", "host",
		),
		Port: config.EnvName(
			prefix, "server", "port",
		),
		ReadTimeout: config.EnvName(
			prefix, "server", "read", "timeout",
		),
		ReadHeaderTimeout: config.EnvName(
			prefix, "server", "read", "header", "timeout",
		),
		WriteTimeout: config.EnvName(
			prefix, "server", "write", "timeout",
		),
		IdleTimeout: config.EnvName(
			prefix, "server", "idle", "timeout",
		),
	}
}
