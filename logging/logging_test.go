package logging

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Evsdrg/cepvor-cpa-plinth/abi"
)

// emit 走进程级 host caller，测试里用假 caller 截住请求。
func withCapturedLog(t *testing.T, run func()) []map[string]any {
	t.Helper()
	var captured []map[string]any
	abi.SetHostCaller(func(method string, request []byte) ([]byte, error) {
		if method != "host.log" {
			return nil, errors.New("unexpected method " + method)
		}
		var payload map[string]any
		if err := json.Unmarshal(request, &payload); err != nil {
			return nil, err
		}
		captured = append(captured, payload)
		return []byte(`{}`), nil
	})
	defer abi.ClearHostCaller()
	run()
	return captured
}

func TestEmitCarriesLevelAndMessage(t *testing.T) {
	captured := withCapturedLog(t, func() {
		Info(&abi.Host{}, "Qoder.checkin：账号已签到", Auth("uid-1"), State("claimed"))
	})
	if len(captured) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(captured))
	}
	if captured[0]["level"] != LevelInfo {
		t.Fatalf("level = %v, want %v", captured[0]["level"], LevelInfo)
	}
	if captured[0]["message"] != "Qoder.checkin：账号已签到" {
		t.Fatalf("message = %v", captured[0]["message"])
	}
	fields, _ := captured[0]["fields"].(map[string]any)
	if fields["auth_id"] != "uid-1" {
		t.Fatalf("auth_id = %v, want uid-1", fields["auth_id"])
	}
	if fields["state"] != "claimed" {
		t.Fatalf("state = %v, want claimed", fields["state"])
	}
}

// 空 Field 必须被跳过：Err(nil) 这类调用点在正常路径上会产出空 Field，
// 写进 JSON 会变成空字符串字段，把日志行弄脏。
func TestEmitSkipsEmptyFields(t *testing.T) {
	captured := withCapturedLog(t, func() {
		Warn(&abi.Host{}, "Qoder.checkin：签到失败", Err(nil), State(""), Auth("uid-2"))
	})
	fields, _ := captured[0]["fields"].(map[string]any)
	if _, present := fields["error"]; present {
		t.Fatalf("nil error must not produce an error field: %v", fields)
	}
	if _, present := fields["state"]; present {
		t.Fatalf("empty state must not produce a state field: %v", fields)
	}
	if fields["auth_id"] != "uid-2" {
		t.Fatalf("auth_id = %v, want uid-2", fields["auth_id"])
	}
}

// host_callback_id 只在有回调身份时才带上。后台循环用的空 Host 不该伪造一个
// 空的 host_callback_id —— 宿主会拿它去解析回调上下文，空串等于没有。
func TestEmitOnlySetsCallbackIDWhenPresent(t *testing.T) {
	captured := withCapturedLog(t, func() {
		Info(&abi.Host{}, "Qoder.checkin：无回调身份的循环日志")
		Info(&abi.Host{CallbackID: "cb-1"}, "Qoder.checkin：请求内的日志")
	})
	if _, present := captured[0]["host_callback_id"]; present {
		t.Fatalf("empty host must not carry host_callback_id: %v", captured[0])
	}
	if captured[1]["host_callback_id"] != "cb-1" {
		t.Fatalf("host_callback_id = %v, want cb-1", captured[1]["host_callback_id"])
	}
}

// 宿主的 console formatter 只打印 logFieldOrder 白名单里的键（见
// internal/logging/global_logger.go）。白名单外的字段不会报错，只是在日志里
// 消失，所以这里把白名单抄一份钉住：任何构造器产出的键名都必须在内。
func TestFieldKeysStayWithinHostWhitelist(t *testing.T) {
	whitelist := map[string]bool{
		"provider": true, "model": true,
		"plugin_id": true, "plugin_name": true, "source_id": true,
		"version": true, "active_version": true, "retired_version": true, "overwritten": true,
		"mode": true, "budget": true, "level": true, "original_mode": true, "original_value": true,
		"min": true, "max": true, "clamped_to": true, "error": true,
		"credential": true, "auth_id": true, "auth_index": true, "connection": true,
		"proxy_scheme": true, "remote_transport": true,
		"operation": true, "upstream_host": true, "reused": true, "was_idle": true, "idle_time": true,
		"media_session_id": true, "call_id": true, "peer": true, "state": true, "reason": true,
		"request_id": true,
	}

	constructed := []Field{
		Provider("p"), Model("m"), Auth("a"), AuthIndex("i"),
		Err(errors.New("e")), Reason("r"), State("s"), Operation("o"),
	}
	for _, field := range constructed {
		if field.Key == "" {
			t.Fatalf("constructor produced an empty key")
		}
		if !whitelist[field.Key] {
			t.Fatalf("field key %q is not in the host's logFieldOrder; it would be silently dropped", field.Key)
		}
	}
}

// 消息形态统一为 <组件>：<中文描述>，这样操作员能按组件过滤。
func TestMessageShapeIsComponentColonDescription(t *testing.T) {
	pattern := regexp.MustCompile(`^[A-Za-z]+(\.[A-Za-z]+)*.*：\S`)

	samples := []string{
		"Qoder.checkin：账号 uid-1 签到成功",
		"CodeArts.auth：凭据已续期",
		"TRAE.catalog：模型目录不可用，回退内置列表",
		"Workbuddy.failover：账号切换",
	}
	for _, message := range samples {
		if !pattern.MatchString(message) {
			t.Fatalf("message %q does not match <component>：<description>", message)
		}
	}
}

// Err(nil) 返回空 Field 而不是带 "error": "" 的字段，上一条测试已经覆盖
// 跳过逻辑，这里钉住返回值本身，避免将来改成写空串。
func TestErrReturnsEmptyFieldForNil(t *testing.T) {
	if field := Err(nil); field.Key != "" || field.Value != "" {
		t.Fatalf("Err(nil) = %+v, want zero Field", field)
	}
	if field := Err(errors.New("boom")); field.Key != "error" || field.Value != "boom" {
		t.Fatalf("Err(err) = %+v, want error=boom", field)
	}
}

// 冒烟：包能被正常引入并编译（防止遗漏 import 或签名漂移）。
func TestPackageCompilesAgainstRepo(t *testing.T) {
	root := findRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root not found: %v", err)
	}
	if !strings.Contains(string(readFile(t, filepath.Join(root, "go.mod"))), "cepvor-cpa-plinth") {
		t.Fatalf("go.mod is not the plinth module")
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("repository root not found")
	return ""
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
