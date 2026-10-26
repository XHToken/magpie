package routerprocess

import "context"

func BackendHints(ctx context.Context) []string {
	if ctx.Err() != nil {
		return nil
	}
	// The verified engine enumerates Metal even in a no-cgo Router build.
	return []string{"metal"}
}
