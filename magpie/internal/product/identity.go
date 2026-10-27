// Package product owns the Wefto name and the narrowly scoped identifiers
// accepted while upgrading an existing API-router installation.
package product

const (
	ID                    = "wefto"
	Name                  = "Wefto"
	DisplayName           = "端云协同"
	LegacyID              = "api-router"
	LegacyProviderID      = "api_router"
	LegacyProviderName    = "API Router"
	LegacyAutostartPrefix = "API-router"
	PlaceholderKey        = "wefto-local-placeholder"
	LegacyPlaceholderKey  = "api-router-local-placeholder"
	UIHeader              = "X-Wefto-UI"
	LegacyUIHeader        = "X-API-Router-UI"
	InstanceHeader        = "X-Wefto-Instance"
	LegacyInstanceHeader  = "X-API-Router-Instance"
	HostLock              = ".wefto-host.lock"
	LegacyHostLock        = ".api-router-host.lock"
)

func IsService(id string) bool  { return id == ID || id == LegacyID }
func IsProvider(id string) bool { return id == ID || id == LegacyProviderID }
func IsProviderName(value any) bool {
	return value == DisplayName || value == Name || value == LegacyProviderName
}
func IsPlaceholder(value any) bool { return value == PlaceholderKey || value == LegacyPlaceholderKey }
