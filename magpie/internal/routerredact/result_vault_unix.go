//go:build linux || darwin

package routerredact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const resultVaultService = "org.xhtoken.wefto.redaction-results.v1"

type resultVaultReference struct {
	Version int    `json:"version"`
	Backend string `json:"backend"`
	Account string `json:"account"`
	Digest  string `json:"digest"`
}

type resultVault interface {
	put(string, []byte) error
	get(string) ([]byte, error)
}

func vaultProtectResultKey(raw []byte, backend string, vault resultVault) ([]byte, error) {
	var bundle resultKeyBundle
	if json.Unmarshal(raw, &bundle) != nil || !validResultAccount(bundle.StoreID) || len(bundle.Master) != 32 || bundle.Version != resultSchema {
		return nil, fail("redaction_store_key_unavailable")
	}
	if err := vault.put(bundle.StoreID, raw); err != nil {
		return nil, fail("redaction_store_key_unavailable")
	}
	// Verify a durable read before publishing the reference or opening the DB.
	kept, err := vault.get(bundle.StoreID)
	if err != nil || string(kept) != string(raw) {
		return nil, fail("redaction_store_key_unavailable")
	}
	digest := sha256.Sum256(raw)
	return json.Marshal(resultVaultReference{1, backend, bundle.StoreID, hex.EncodeToString(digest[:])})
}

func vaultUnprotectResultKey(raw []byte, backend string, vault resultVault) ([]byte, error) {
	var ref resultVaultReference
	if len(raw) > 16384 || json.Unmarshal(raw, &ref) != nil || ref.Version != 1 || ref.Backend != backend || !validResultAccount(ref.Account) {
		return nil, fail("redaction_store_key_unavailable")
	}
	kept, err := vault.get(ref.Account)
	if err != nil {
		return nil, fail("redaction_store_key_unavailable")
	}
	digest := sha256.Sum256(kept)
	var bundle resultKeyBundle
	if hex.EncodeToString(digest[:]) != ref.Digest || json.Unmarshal(kept, &bundle) != nil || bundle.StoreID != ref.Account || len(bundle.Master) != 32 || bundle.Version != resultSchema {
		return nil, fail("redaction_store_key_unavailable")
	}
	return kept, nil
}

func validResultAccount(id string) bool {
	value, err := hex.DecodeString(id)
	return err == nil && len(value) == 16 && hex.EncodeToString(value) == id
}
