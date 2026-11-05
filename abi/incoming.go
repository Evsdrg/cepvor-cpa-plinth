package abi

import (
	"bytes"
	"encoding/json"
	"sync"
)

// rawIncoming guards the payload the host supplied for this invocation.
//
// It needs its own lock rather than reusing anything else because a Host is
// reachable from more than one goroutine: the host calls a management route such
// as the status page once per HTTP request, and each route handler touches the
// SAME Host value through `credentialOf` -> GetAuth -> rememberIncoming. Test
// TestStatusPageRenewsOnceForConcurrentCallers drives exactly that shape with six
// concurrent callers through one Host.
//
// The zero value is usable, so a Host built with &Host{} (the background
// schedulers do this) needs no initialisation.
type rawIncoming struct {
	mu  sync.RWMutex
	raw json.RawMessage
}

// set records raw only when nothing has been recorded yet.
//
// First-wins is deliberate: within one invocation every save targets the same
// credential, so the first auth file seen is the right base for merging the
// members this plugin does not own. A second auth file arriving mid-invocation
// must not replace it.
func (r *rawIncoming) set(raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(bytes.TrimSpace(r.raw)) == 0 {
		r.raw = append(json.RawMessage(nil), raw...)
	}
}

// get returns the recorded payload. The copy matters: callers hand it to
// credjson.MergePreserved and to withFileMembers, and returning the internal
// slice would let an appending caller mutate the shared backing array.
func (r *rawIncoming) get() json.RawMessage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), r.raw...)
}
