//go:build !windows && !darwin

package routerprocess

import "context"

func BackendHints(context.Context) []string { return nil }
