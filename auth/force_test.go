package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Evsdrg/cepvor-cpa-plinth/abi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Force must renew while the recorded expiry still looks valid.
func TestForceRefreshesWhileEnsureWouldSkip(t *testing.T) {
	calls := 0
	policy := Policy{
		Provider: "test",
		Lead:     time.Minute,
		Expiry: func(storage json.RawMessage) (time.Time, bool) {
			return time.Now().Add(24 * time.Hour), true
		},
		Refresh: func(h *abi.Host, request pluginapi.AuthRefreshRequest) (pluginapi.AuthRefreshResponse, error) {
			calls++
			return pluginapi.AuthRefreshResponse{Auth: pluginapi.AuthData{StorageJSON: json.RawMessage(`{"token":"new"}`)}}, nil
		},
	}
	r := New(policy)
	host := newForceHost(t)
	request := Request{Name: "a.json", StorageJSON: json.RawMessage(`{"token":"old"}`)}

	if result, err := r.Ensure(host, request); err != nil || result.Attempted {
		t.Fatalf("Ensure touched a comfortably valid credential: attempted=%v err=%v", result.Attempted, err)
	}
	if calls != 0 {
		t.Fatalf("Ensure made %d vendor calls, want 0", calls)
	}
	result, err := r.Force(host, request)
	if err != nil {
		t.Fatalf("Force: %v", err)
	}
	if !result.Refreshed || calls != 1 {
		t.Fatalf("Force refreshed=%v calls=%d, want true/1", result.Refreshed, calls)
	}
}

// Force must not bypass the terminal verdict.
func TestForceHonoursDeadCredential(t *testing.T) {
	calls := 0
	policy := Policy{
		Provider: "test",
		Lead:     time.Minute,
		Expiry: func(storage json.RawMessage) (time.Time, bool) {
			return time.Now().Add(-time.Hour), true
		},
		Refresh: func(h *abi.Host, request pluginapi.AuthRefreshRequest) (pluginapi.AuthRefreshResponse, error) {
			calls++
			return pluginapi.AuthRefreshResponse{}, abi.HTTPError("unauthorized", http.StatusUnauthorized, "refresh rejected")
		},
	}
	r := New(policy)
	host := newForceHost(t)
	request := Request{Name: "a.json", StorageJSON: json.RawMessage(`{"token":"dead"}`)}

	if _, err := r.Ensure(host, request); err == nil {
		t.Fatal("Ensure should report the terminal failure")
	}
	if !IsTerminal(abi.HTTPError("unauthorized", http.StatusUnauthorized, "refresh rejected")) {
		t.Fatal("test precondition: 401 must classify as terminal")
	}
	before := calls
	if result, err := r.Force(host, request); err == nil || !result.Dead {
		t.Fatalf("Force must report the credential dead: dead=%v err=%v", result.Dead, err)
	}
	if calls != before {
		t.Fatalf("Force retried a dead credential: calls %d -> %d", before, calls)
	}
}

// newForceHost installs the minimum fake transport: auth.save and host.log.
func newForceHost(t *testing.T) *abi.Host {
	t.Helper()
	abi.SetHostCaller(func(method string, request []byte) ([]byte, error) {
		switch method {
		case pluginabi.MethodHostAuthSave:
			var payload struct {
				Name string          `json:"name"`
				JSON json.RawMessage `json:"json"`
			}
			if err := json.Unmarshal(request, &payload); err != nil {
				return nil, err
			}
			return abi.OK(map[string]any{"name": payload.Name, "path": "/auths/" + payload.Name})
		case pluginabi.MethodHostLog:
			return abi.OK(map[string]any{})
		}
		return nil, fmt.Errorf("unexpected host callback %s", method)
	})
	t.Cleanup(abi.ClearHostCaller)
	return &abi.Host{CallbackID: "cb-force", PluginID: "demo"}
}
