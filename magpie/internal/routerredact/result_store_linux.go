package routerredact

import (
	"context"
	"mime"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

func resultSystemAlias(string, os.FileInfo) bool { return false }

// Linux POSIX ACL permissions are bounded by the group mask reflected in
// st_mode. The required 0700/0600 mask gives named users/groups no access.
func clearResultACL(string) error  { return nil }
func verifyResultACL(string) error { return nil }

func protectResultKey(raw []byte) ([]byte, error) {
	return vaultProtectResultKey(raw, "secret-service", linuxResultVault{})
}
func unprotectResultKey(raw []byte) ([]byte, error) {
	return vaultUnprotectResultKey(raw, "secret-service", linuxResultVault{})
}

const resultSecretService = "org.freedesktop.Secret.Service"

type linuxResultVault struct{}

// Field order is the standard Secret Service (oayays) wire signature.
type resultServiceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func resultSecretAttributes(id string) map[string]string {
	return map[string]string{"application": resultVaultService, "store": id}
}

func withResultSecretSession(use func(context.Context, *dbus.Conn, dbus.BusObject, dbus.ObjectPath) error) error {
	// HOME isolation does not isolate a login keyring. Native vault tests opt
	// into a private bus explicitly; ordinary test binaries never use this one.
	if os.Getenv("MAGPIE_TEST_SANDBOX") != "" {
		return fail("redaction_store_key_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return fail("redaction_store_key_unavailable")
	}
	defer conn.Close()
	service := conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets")
	var output dbus.Variant
	var session dbus.ObjectPath
	if service.CallWithContext(ctx, resultSecretService+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session) != nil || session == "/" || !session.IsValid() {
		return fail("redaction_store_key_unavailable")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		conn.Object("org.freedesktop.secrets", session).CallWithContext(cleanup, "org.freedesktop.Secret.Session.Close", 0)
	}()
	return use(ctx, conn, service, session)
}

func (linuxResultVault) put(id string, raw []byte) error {
	return withResultSecretSession(func(ctx context.Context, conn *dbus.Conn, service dbus.BusObject, session dbus.ObjectPath) error {
		var collection dbus.ObjectPath
		if service.CallWithContext(ctx, resultSecretService+".ReadAlias", 0, "default").Store(&collection) != nil || collection == "/" || !collection.IsValid() {
			return fail("redaction_store_key_unavailable")
		}
		object := conn.Object("org.freedesktop.secrets", collection)
		var locked dbus.Variant
		if object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.Secret.Collection", "Locked").Store(&locked) != nil || locked.Value() != false {
			return fail("redaction_store_key_unavailable")
		}
		var found, unavailable []dbus.ObjectPath
		if service.CallWithContext(ctx, resultSecretService+".SearchItems", 0, resultSecretAttributes(id)).Store(&found, &unavailable) != nil || len(found)+len(unavailable) != 0 {
			return fail("redaction_store_key_unavailable")
		}
		properties := map[string]dbus.Variant{
			"org.freedesktop.Secret.Item.Label":      dbus.MakeVariant("Wefto private recognition results"),
			"org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(resultSecretAttributes(id)),
		}
		secret := resultServiceSecret{session, []byte{}, raw, "text/plain; charset=utf-8"}
		var item, prompt dbus.ObjectPath
		if object.CallWithContext(ctx, "org.freedesktop.Secret.Collection.CreateItem", 0, properties, secret, false).Store(&item, &prompt) != nil || item == "/" || !item.IsValid() || prompt != "/" {
			return fail("redaction_store_key_unavailable")
		}
		return nil
	})
}

func (linuxResultVault) get(id string) ([]byte, error) {
	var raw []byte
	err := withResultSecretSession(func(ctx context.Context, conn *dbus.Conn, service dbus.BusObject, session dbus.ObjectPath) error {
		var found, locked []dbus.ObjectPath
		if service.CallWithContext(ctx, resultSecretService+".SearchItems", 0, resultSecretAttributes(id)).Store(&found, &locked) != nil || len(found) != 1 || len(locked) != 0 || found[0] == "/" || !found[0].IsValid() {
			return fail("redaction_store_key_unavailable")
		}
		var secret resultServiceSecret
		if conn.Object("org.freedesktop.secrets", found[0]).CallWithContext(ctx, "org.freedesktop.Secret.Item.GetSecret", 0, session).Store(&secret) != nil || secret.Session != session || len(secret.Parameters) != 0 || len(secret.Value) == 0 || len(secret.Value) > 16384 {
			return fail("redaction_store_key_unavailable")
		}
		// GNOME Keyring normalizes stored content types to text/plain. Other
		// implementations retain the supplied type; neither changes the bytes.
		kind, _, err := mime.ParseMediaType(secret.ContentType)
		if err != nil || (kind != "text/plain" && kind != "application/octet-stream") {
			return fail("redaction_store_key_unavailable")
		}
		raw = append([]byte(nil), secret.Value...)
		return nil
	})
	return raw, err
}
