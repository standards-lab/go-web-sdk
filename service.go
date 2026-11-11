package web

import (
	"context"
	"io"
	"log/slog"
	"slices"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process"
)

// Service is a web service's composition root as [Service.Run] runs it:
// the graph, the nodes Run reads by role, and the roots that select what
// the service builds. Every field is required.
type Service struct {
	// Graph is the graph every node below is defined on.
	Graph *graph.Graph
	// LifecycleConfig is the node whose value configures the
	// [lifecycle.Coordinator] Run drives, a finalized [lifecycle.Config].
	LifecycleConfig *graph.Node[lifecycle.Config]
	// Logger is the node whose value Run reports the service's outcome on.
	// Roots or Server must reach it.
	Logger *graph.Node[*slog.Logger]
	// Server is the node whose value's bound address Run reports once the
	// service is ready.
	Server *graph.Node[*Server]
	// Roots are the nodes the service builds, beside Server and
	// LifecycleConfig, which join every Build.
	Roots []graph.Ref
}

// Run builds Roots, Server, and LifecycleConfig from Graph, runs the System
// under a [lifecycle.Coordinator] configured with LifecycleConfig's value
// until ctx ends, and returns the process exit code. It reports its outcome
// once:
//
//   - once startup completes, it logs "server ready" on Logger's value, with
//     the server's bound address as addr
//   - when ctx ends and the System drains cleanly, it logs "server stopped"
//     and returns [process.ExitOK]
//   - when startup, a [lifecycle.Monitored] value at runtime, or shutdown
//     fails, it logs "service failed" with the Coordinator's error, which
//     names the failing node, and returns [process.ExitFailure]; a failed
//     startup logs no "server ready"
//   - when the Build fails, it writes "service failed: <error>" to stderr,
//     the error naming the failing node, and returns [process.ExitFailure].
//     It logs nothing, since the logger node may be what failed, and starts
//     nothing.
//
// stderr is the process's standard error, or a test's buffer. Run panics,
// before anything starts, on a wiring mistake: a nil Graph,
// LifecycleConfig, Logger, or Server, an empty Roots, or a Build that did
// not reach Logger.
func (s Service) Run(ctx context.Context, stderr io.Writer) int {
	switch {
	case s.Graph == nil:
		panic("web: Service.Run requires a Graph")
	case s.LifecycleConfig == nil:
		panic("web: Service.Run requires a LifecycleConfig node")
	case s.Logger == nil:
		panic("web: Service.Run requires a Logger node")
	case s.Server == nil:
		panic("web: Service.Run requires a Server node")
	case len(s.Roots) == 0:
		panic("web: Service.Run requires at least one of Roots")
	}

	// Clip so the appends never write into the caller's backing array.
	sys, err := s.Graph.Build(append(slices.Clip(s.Roots), s.Server, s.LifecycleConfig)...)
	if err != nil {
		return process.Fail(stderr, "service failed", err)
	}
	if !sys.Has(s.Logger) {
		panic("web: Service.Run: the Build did not reach the Logger node; add it to Roots or have a root use it")
	}

	log := sys.Get(s.Logger)
	srv := sys.Get(s.Server)
	lc := lifecycle.New(sys, sys.Get(s.LifecycleConfig))
	lc.OnReady(func() {
		log.Info("server ready", "addr", srv.Addr())
	})

	if err := lc.Run(ctx); err != nil {
		log.Error("service failed", "error", err)
		return process.ExitFailure
	}
	log.Info("server stopped")
	return process.ExitOK
}
