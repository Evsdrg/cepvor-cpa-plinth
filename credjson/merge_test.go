package credjson

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("result is not a JSON object: %v (%s)", err, raw)
	}
	return out
}

// The host reads priority and weight from the auth file, so a plugin-side
// rewrite must not drop them: that is what reset the credential routing tiers.
func TestMergePreservedKeepsHostOwnedMembers(t *testing.T) {
	base := []byte(`{"priority":6,"weight":3,"access_token":"old","type":"cline"}`)
	overlay := []byte(`{"access_token":"new","type":"cline"}`)

	got := decode(t, MergePreserved(base, overlay))
	if got["priority"] != float64(6) {
		t.Errorf("priority = %v, want 6", got["priority"])
	}
	if got["weight"] != float64(3) {
		t.Errorf("weight = %v, want 3", got["weight"])
	}
	if got["access_token"] != "new" {
		t.Errorf("access_token = %v, want the overlay value", got["access_token"])
	}
}

func TestMergePreservedOverlayWinsOnConflict(t *testing.T) {
	base := []byte(`{"priority":4,"nickname":"old"}`)
	overlay := []byte(`{"priority":"5","nickname":"new"}`)

	got := decode(t, MergePreserved(base, overlay))
	if got["priority"] != "5" {
		t.Errorf("priority = %v, want the overlay value", got["priority"])
	}
	if got["nickname"] != "new" {
		t.Errorf("nickname = %v, want the overlay value", got["nickname"])
	}
}

func TestMergePreservedReturnsOverlayUnchangedWhenNothingToAdd(t *testing.T) {
	base := []byte(`{"priority":6}`)
	overlay := []byte(`{"priority":6,"access_token":"token"}`)

	got := MergePreserved(base, overlay)
	if string(got) != string(overlay) {
		t.Errorf("result = %s, want the overlay bytes untouched", got)
	}
}

// Malformed or non-object input must never grow or corrupt an auth file.
func TestMergePreservedRejectsNonObjects(t *testing.T) {
	cases := []struct {
		name    string
		base    []byte
		overlay []byte
	}{
		{"nil base", nil, []byte(`{"a":1}`)},
		{"empty base", []byte("  "), []byte(`{"a":1}`)},
		{"malformed base", []byte(`{"a":`), []byte(`{"a":1}`)},
		{"array base", []byte(`[1,2]`), []byte(`{"a":1}`)},
		{"array overlay", []byte(`{"priority":6}`), []byte(`[1,2]`)},
		{"malformed overlay", []byte(`{"priority":6}`), []byte(`{"a":`)},
		{"empty overlay", []byte(`{"priority":6}`), nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := MergePreserved(testCase.base, testCase.overlay)
			if string(got) != string(testCase.overlay) {
				t.Errorf("result = %s, want the overlay unchanged (%s)", got, testCase.overlay)
			}
		})
	}
}
