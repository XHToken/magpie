//go:build !windows && !linux && !darwin

package routerredact

import "os"

func resultKeyFile() string { return "keys.vault" }

// Other platforms require an accepted system key and permissions backend. A
// plaintext key file or chmod-only fallback must never silently enable egress.
func checkResultPath(string) error       { return fail("redaction_store_platform_unsupported") }
func secureResultDirectory(string) error { return fail("redaction_store_platform_unsupported") }
func verifyResultPrivate(string) error   { return fail("redaction_store_platform_unsupported") }
func protectResultKey([]byte) ([]byte, error) {
	return nil, fail("redaction_store_platform_unsupported")
}
func unprotectResultKey([]byte) ([]byte, error) {
	return nil, fail("redaction_store_platform_unsupported")
}
func lockResultStore(string) (*os.File, error) {
	return nil, fail("redaction_store_platform_unsupported")
}
