module github.com/Evsdrg/cepvor-cpa-plinth

go 1.26.0

// Tracks upstream directly. The SDK uses only pluginapi types that exist in
// the public v8 line (AuthData, AuthRefreshRequest, HostAuthSaveRequest,
// HTTPRequest/Response, ModelInfo, Metadata), so it carries no fork
// dependency and can be consumed with a plain version requirement.
//
// Plugins that need the fork's pluginapi.ConfigField extensions (Label,
// Default, OptionsEndpoint) replace this module with their local fork, the
// same way they already do for the host.
require github.com/router-for-me/CLIProxyAPI/v8 v8.0.32
