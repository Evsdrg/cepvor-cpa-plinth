package config

import (
	"reflect"
	"testing"
)

func TestBoolAcceptsEveryYAMLForm(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		fallback bool
		want     bool
	}{
		{"bool true", true, false, true},
		{"bool false", false, true, false},
		{"int 1", 1, false, true},
		{"int 0", 0, true, false},
		{"int64 1", int64(1), false, true},
		{"float64 1", 1.0, false, true},
		{"float64 0", 0.0, true, false},
		{"string true", "true", false, true},
		{"string TRUE", "TRUE", false, true},
		{"string yes", " yes ", false, true},
		{"string on", "on", false, true},
		{"string 1", "1", false, true},
		{"string false", "false", true, false},
		{"string 0", "0", true, false},
		{"unrecognised string", "maybe", false, false},
		{"unrecognised keeps fallback", "maybe", true, true},
		{"nil keeps fallback", nil, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Bool(tc.value, tc.fallback); got != tc.want {
				t.Fatalf("Bool(%#v, %v) = %v, want %v", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestIntAcceptsEveryYAMLForm(t *testing.T) {
	cases := []struct {
		value    any
		fallback int
		want     int
	}{
		{42, 0, 42},
		{int64(42), 0, 42},
		{42.9, 0, 42},
		{"42", 0, 42},
		{" 42 ", 0, 42},
		{"abc", -1, -1},
		{"", -1, -1},
		{nil, -1, -1},
	}
	for _, tc := range cases {
		if got := Int(tc.value, tc.fallback); got != tc.want {
			t.Fatalf("Int(%#v, %d) = %d, want %d", tc.value, tc.fallback, got, tc.want)
		}
	}
}

// float64 截断而不是四舍五入：配置项是整数语义，YAML 把 300000 解析成
// float64 只是类型推断的结果，截断与整数写法等价。
func TestIntTruncatesRatherThanRounds(t *testing.T) {
	if got := Int(2.9, 0); got != 2 {
		t.Fatalf("Int(2.9) = %d, want 2", got)
	}
}

func TestStringAndMapString(t *testing.T) {
	if got := String("  hello  ", "x"); got != "hello" {
		t.Fatalf("String = %q", got)
	}
	if got := String("   ", "x"); got != "x" {
		t.Fatalf("whitespace-only must fall back, got %q", got)
	}
	if got := String(42, "x"); got != "x" {
		t.Fatalf("non-string must fall back, got %q", got)
	}

	record := map[string]any{"name": " value ", "num": 7}
	if got := MapString(record, "name"); got != "value" {
		t.Fatalf("MapString(name) = %q", got)
	}
	if got := MapString(record, "num"); got != "" {
		t.Fatalf("MapString must ignore non-strings, got %q", got)
	}
	if got := MapString(record, "missing"); got != "" {
		t.Fatalf("MapString(missing) = %q", got)
	}
	if got := MapString(nil, "name"); got != "" {
		t.Fatalf("MapString(nil) = %q", got)
	}
}

func TestListDistinguishesEmptyFromAbsent(t *testing.T) {
	if got := List(nil); got != nil {
		t.Fatalf("List(nil) = %v, want nil", got)
	}
	if got := List(""); got != nil {
		t.Fatalf("List(\"\") = %v, want nil", got)
	}
	if got := List([]any{}); got != nil {
		t.Fatalf("List(empty slice) = %v, want nil", got)
	}

	if got := List("a, b ,c"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("List(comma string) = %v", got)
	}
	if got := List([]any{"a", 42, "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("List(mixed slice) = %v", got)
	}
	if got := List([]string{"a", " b "}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("List(string slice) = %v", got)
	}
}

func TestHoursAcceptsMultipleSeparators(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []int
	}{
		{"comma", "9,21", []int{9, 21}},
		{"space", "9 21", []int{9, 21}},
		{"semicolon", "9;21", []int{9, 21}},
		{"tab", "9\t21", []int{9, 21}},
		{"newline", "9\n21", []int{9, 21}},
		{"mixed spacing", " 21 , 9 ; 15 ", []int{9, 15, 21}},
		{"dedupes", "9,9,21", []int{9, 21}},
		{"drops out of range", "9,24,-1,21", []int{9, 21}},
		{"string elements in array", []any{"9", "21", 15}, []int{9, 15, 21}},
		{"int slice", []int{21, 9}, []int{9, 21}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Hours(tc.value, []int{7})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Hours(%#v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestHoursFallsBackWhenNothingUsable(t *testing.T) {
	fallback := []int{9, 21}
	for _, value := range []any{nil, "", "abc", []any{}, 3.5, "24,25"} {
		if got := Hours(value, fallback); !reflect.DeepEqual(got, fallback) {
			t.Fatalf("Hours(%#v) = %v, want fallback %v", value, got, fallback)
		}
	}
}

// nil 表示「从未配置」，空切片表示「全取消」。合并两者会让误清一次勾选
// 导致整个 provider 静默消失。
func TestSelectionDistinguishesAbsentFromEmpty(t *testing.T) {
	if got := Selection(nil); got != nil {
		t.Fatalf("Selection(nil) = %v, want nil", got)
	}
	empty := Selection([]any{})
	if empty == nil {
		t.Fatal("Selection(empty slice) must be a non-nil empty slice")
	}
	if len(empty) != 0 {
		t.Fatalf("Selection(empty slice) = %v, want empty", empty)
	}
}

func TestSelectionParsesRowsAndKeepsFirstDuplicate(t *testing.T) {
	value := []any{
		map[string]any{"upstream": "DeepSeek-V4-Pro", "name": "ds-v4-pro"},
		map[string]any{"upstream": "DeepSeek-V4-Pro", "name": "ds-v4-pro-renamed"},
		map[string]any{"upstream": "  glm-5.3  ", "name": " my-glm "},
		map[string]any{"name": "no-upstream"},
		map[string]any{"upstream": ""},
		"not a map",
	}
	got := Selection(value)
	want := []SelectionEntry{
		{Upstream: "DeepSeek-V4-Pro", Name: "ds-v4-pro"},
		{Upstream: "glm-5.3", Name: "my-glm"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Selection = %+v, want %+v", got, want)
	}
}

func TestSelectionPassesTypedSliceThrough(t *testing.T) {
	typed := []SelectionEntry{{Upstream: "a", Name: "b"}}
	if got := Selection(typed); !reflect.DeepEqual(got, typed) {
		t.Fatalf("Selection(typed) = %+v", got)
	}
}

// 名称留空是合法状态：由各插件回落到自己派生的注册名，不是错误。
func TestSelectionKeepsEmptyName(t *testing.T) {
	got := Selection([]any{map[string]any{"upstream": "only-upstream"}})
	want := []SelectionEntry{{Upstream: "only-upstream", Name: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Selection = %+v, want %+v", got, want)
	}
}
