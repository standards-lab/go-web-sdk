package web_test

import (
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

func TestNewEnv_ComposesNamesFromPrefix(t *testing.T) {
	env := web.NewEnv("herald")

	for _, tc := range []struct {
		got  string
		want string
	}{
		{env.Host, "HERALD_SERVER_HOST"},
		{env.Port, "HERALD_SERVER_PORT"},
		{env.ReadTimeout, "HERALD_SERVER_READ_TIMEOUT"},
		{env.ReadHeaderTimeout, "HERALD_SERVER_READ_HEADER_TIMEOUT"},
		{env.WriteTimeout, "HERALD_SERVER_WRITE_TIMEOUT"},
		{env.IdleTimeout, "HERALD_SERVER_IDLE_TIMEOUT"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestNewEnv_EmptyPrefixReturnsZeroEnv(t *testing.T) {
	if env := web.NewEnv(""); env != (web.Env{}) {
		t.Errorf("NewEnv(\"\") = %+v, want the zero Env (overrides disabled)", env)
	}
}
