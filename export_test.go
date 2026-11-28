package web

import "context"

func RunContext(ctx context.Context) (context.Context, bool) {
	return runContext(ctx)
}
