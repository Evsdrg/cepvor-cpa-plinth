package credjson

import (
	"bytes"
	"encoding/json"
)

// MergePreserved returns overlay with every top-level member of base that
// overlay does not define.
//
// The auth file is shared state. The plugin owns the credential members it
// writes, while the host owns routing members that live in the same file —
// `priority` (sdk/cliproxy/auth/priority.go) and `weight`
// (sdk/cliproxy/auth/weight.go) are read from the file on load and drive
// credential selection. `host.auth.save` replaces the whole file with the bytes
// a plugin hands it, so a plugin that serialises only its own credential struct
// silently erases those members on the next self-initiated refresh: the
// credential keeps working, but its routing tier quietly resets to the default.
//
// Unknown members therefore ride along untouched. Overlay always wins for the
// members it defines, so a plugin can still rename or drop a field it owns.
//
// Both inputs must be JSON objects. Anything else (nil, empty, malformed, or a
// non-object) makes the function return overlay unchanged, so malformed input
// can never grow an auth file.
func MergePreserved(base, overlay []byte) []byte {
	trimmedOverlay := bytes.TrimSpace(overlay)
	if len(trimmedOverlay) == 0 {
		return overlay
	}
	var baseMembers map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(bytes.TrimSpace(base), &baseMembers); errUnmarshal != nil || baseMembers == nil {
		return overlay
	}
	var overlayMembers map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(trimmedOverlay, &overlayMembers); errUnmarshal != nil || overlayMembers == nil {
		return overlay
	}

	missing := false
	for key := range baseMembers {
		if _, exists := overlayMembers[key]; exists {
			continue
		}
		overlayMembers[key] = baseMembers[key]
		missing = true
	}
	if !missing {
		return overlay
	}

	merged, errMarshal := json.Marshal(overlayMembers)
	if errMarshal != nil {
		return overlay
	}
	return merged
}
