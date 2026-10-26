package routerconfig

import "strings"

// ReservedCloudHeader identifies headers owned by the HTTP transport or wire
// codec. Configuration validation and dispatch enforce this same boundary.
func ReservedCloudHeader(key string) bool {
	switch strings.ToLower(key) {
	case "host", "content-length", "connection", "transfer-encoding", "upgrade", "proxy-authorization", "proxy-connection", "cookie", "set-cookie", "trailer", "te", "content-type", "accept-encoding":
		return true
	}
	return false
}

// ReservedCloudBodyField identifies options that can change the chosen target,
// conversation, tools or remote state. Extra options must never replace them.
func ReservedCloudBodyField(key string) bool {
	switch strings.ToLower(strings.TrimPrefix(key, "!")) {
	case "model", "models", "stream", "messages", "input", "contents", "instructions", "system", "systeminstruction", "system_instruction", "tools", "tool_choice", "functions", "function_call", "previous_response_id", "conversation", "background", "store", "modalities", "audio", "image", "images", "video", "web_search_options", "web_search", "plugins", "mcp_servers", "prompt", "tool_resources", "data_sources", "attachments", "file_ids", "provider", "route", "fallback", "fallbacks", "routing":
		return true
	}
	return false
}
