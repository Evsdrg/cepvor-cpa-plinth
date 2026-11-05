package abi

import (
	"encoding/json"
	"testing"
)

// The host marshals the auth request structs directly, so []byte members travel
// base64-encoded under their Go field names. NewHost has to recover the file
// bytes from that shape: they are what SaveAuth merges the plugin's own
// credential into, and losing them would reset the credential's routing tier on
// every self-initiated refresh.
func TestNewHostCapturesParseRawJSON(t *testing.T) {
	raw := []byte(`{"priority":6,"access_token":"token"}`)
	payload, errMarshal := json.Marshal(struct {
		HostCallbackID string `json:"host_callback_id"`
		Provider       string
		RawJSON        []byte
	}{HostCallbackID: "cb-1", Provider: "cline", RawJSON: raw})
	if errMarshal != nil {
		t.Fatalf("marshal payload: %v", errMarshal)
	}

	host := NewHost(payload)
	if host.CallbackID != "cb-1" {
		t.Errorf("CallbackID = %q, want cb-1", host.CallbackID)
	}
	if string(host.IncomingPayload()) != string(raw) {
		t.Errorf("Incoming = %s, want %s", host.IncomingPayload(), raw)
	}
}

func TestNewHostFallsBackToStorageJSON(t *testing.T) {
	raw := []byte(`{"priority":4,"refresh_token":"token"}`)
	payload, errMarshal := json.Marshal(struct {
		AuthID      string
		StorageJSON []byte
	}{AuthID: "auth-1", StorageJSON: raw})
	if errMarshal != nil {
		t.Fatalf("marshal payload: %v", errMarshal)
	}

	host := NewHost(payload)
	if string(host.IncomingPayload()) != string(raw) {
		t.Errorf("Incoming = %s, want %s", host.IncomingPayload(), raw)
	}
}

// A payload with no auth material (executor calls, identifier probes) must not
// invent one: SaveAuth then writes exactly what the plugin produced.
func TestNewHostWithoutAuthMaterialLeavesIncomingEmpty(t *testing.T) {
	payload, errMarshal := json.Marshal(struct {
		Model string
	}{Model: "GLM-5.2-Oauth"})
	if errMarshal != nil {
		t.Fatalf("marshal payload: %v", errMarshal)
	}

	if host := NewHost(payload); len(host.IncomingPayload()) != 0 {
		t.Errorf("Incoming = %s, want empty", host.IncomingPayload())
	}
}

// A management route reaches the credential through host.auth.get, so the file
// has to be remembered there too; the first known file wins because every save
// in one invocation targets the same credential.
func TestRememberIncomingKeepsFirstKnownFile(t *testing.T) {
	host := &Host{}
	host.rememberIncoming([]byte(`{"priority":6}`))
	host.rememberIncoming([]byte(`{"priority":1}`))
	if string(host.IncomingPayload()) != `{"priority":6}` {
		t.Errorf("Incoming = %s, want the first file", host.IncomingPayload())
	}

	host.rememberIncoming(nil)
	host.rememberIncoming([]byte("  "))
	if string(host.IncomingPayload()) != `{"priority":6}` {
		t.Errorf("Incoming = %s, want the first file after empty inputs", host.IncomingPayload())
	}
}

// The quota and model structs tag their storage member snake_case while the auth
// structs leave it under the Go field name; both spellings reach SaveAuth.
func TestNewHostAcceptsSnakeCaseStorage(t *testing.T) {
	raw := []byte(`{"priority":4,"refresh_token":"token"}`)
	payload, errMarshal := json.Marshal(struct {
		AuthIndex   string `json:"auth_index"`
		StorageJSON []byte `json:"storage_json,omitempty"`
	}{AuthIndex: "auth-1", StorageJSON: raw})
	if errMarshal != nil {
		t.Fatalf("marshal payload: %v", errMarshal)
	}

	host := NewHost(payload)
	if string(host.IncomingPayload()) != string(raw) {
		t.Errorf("Incoming = %s, want %s", host.IncomingPayload(), raw)
	}
}
