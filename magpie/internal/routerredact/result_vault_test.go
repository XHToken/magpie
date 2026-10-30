//go:build linux || darwin

package routerredact

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

type fixtureResultVault struct {
	items  map[string][]byte
	locked bool
}

func (v *fixtureResultVault) put(id string, value []byte) error {
	if v.locked || v.items[id] != nil {
		return errors.New("fixture keyring is locked or item already exists")
	}
	v.items[id] = append([]byte(nil), value...)
	return nil
}
func (v *fixtureResultVault) get(id string) ([]byte, error) {
	if v.locked || v.items[id] == nil {
		return nil, errors.New("fixture keyring locked or missing item")
	}
	return append([]byte(nil), v.items[id]...), nil
}

type vaultResultKeyProtector struct {
	backend string
	vault   resultVault
}

func (p vaultResultKeyProtector) ProtectResultKey(raw []byte) ([]byte, error) {
	return vaultProtectResultKey(raw, p.backend, p.vault)
}
func (p vaultResultKeyProtector) UnprotectResultKey(raw []byte) ([]byte, error) {
	return vaultUnprotectResultKey(raw, p.backend, p.vault)
}

func TestSystemVaultReferenceContainsNoMasterKeyAndAuthenticates(t *testing.T) {
	for _, backend := range []string{"secret-service", "keychain"} {
		t.Run(backend, func(t *testing.T) {
			vault := &fixtureResultVault{items: map[string][]byte{}}
			bundle := resultKeyBundle{resultSchema, "0123456789abcdef0123456789abcdef", bytes.Repeat([]byte{71}, 32)}
			raw, _ := json.Marshal(bundle)
			ref, err := vaultProtectResultKey(raw, backend, vault)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(ref, []byte("Master")) || bytes.Contains(ref, []byte("R0dHR0dH")) {
				t.Fatal("key reference contains master key")
			}
			got, err := vaultUnprotectResultKey(ref, backend, vault)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("key not recovered from system vault", err)
			}
			vault.locked = true
			if _, err := vaultUnprotectResultKey(ref, backend, vault); err == nil {
				t.Fatal("locked keyring accepted")
			}
			vault.locked = false
			vault.items[bundle.StoreID][len(raw)-2] ^= 1
			if _, err := vaultUnprotectResultKey(ref, backend, vault); err == nil {
				t.Fatal("changed vault secret accepted")
			}
			if _, err := vaultUnprotectResultKey(ref, "wrong-backend", vault); err == nil {
				t.Fatal("wrong key provider accepted")
			}
		})
	}
}
