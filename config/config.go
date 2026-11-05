// Package config 归一化插件配置值。
//
// 插件的配置来自宿主传下来的 YAML，先被解析成 map[string]any 再逐键取值。
// YAML 的类型推断让同一个字段可能是 bool、int、float64 或 string
// （`checkin_auto: true` / `checkin_auto: 1` / `checkin_auto: "yes"` 都是合法写法），
// 每个插件各写一份强转逻辑时，同一份配置在不同插件里会得到不同结果。
//
// 这里的函数只做一件事：把任意合理写法归一到目标类型，识别不了就返回调用方
// 给的 fallback。它们不读文件、不碰宿主，是纯函数，可以完全确定地测试。
package config

import (
	"sort"
	"strconv"
	"strings"
)

// Bool 把 YAML 布尔写法归一。数值按「非零即真」处理。
//
// 数值分支不能省：YAML 里不加引号的 `1` 会被解析成 int 而不是 bool。
// 只认 bool 与 string 时，`checkin_auto: 1` 会落到 fallback，与写成
// `checkin_auto: true` 的语义相反。
func Bool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case nil:
		return fallback
	case bool:
		return typed
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case float32:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "on", "1":
			return true
		case "false", "no", "off", "0":
			return false
		}
	}
	return fallback
}

// Int 把 YAML 数值写法归一。无法识别时返回 fallback。
//
// float64 直接截断而不是四舍五入：配置项都是整数语义（毫秒数、token 上限），
// YAML 把 `300000` 解析成 float64 只是类型推断的结果，截断与原本的整数写法
// 等价。
func Int(value any, fallback int) int {
	switch typed := value.(type) {
	case nil:
		return fallback
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		if parsed, errParse := strconv.Atoi(strings.TrimSpace(typed)); errParse == nil {
			return parsed
		}
	}
	return fallback
}

// String 取字符串配置值；空串或非字符串时返回 fallback。
func String(value any, fallback string) string {
	if text, ok := value.(string); ok {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

// MapString 从 map 里取一个字符串值，缺失或为 nil 时返回空串。
// 与 String 的区别是它不 TrimSpace 之后判空——调用方通常自己决定空串的语义。
func MapString(record map[string]any, key string) string {
	if record == nil {
		return ""
	}
	value, ok := record[key]
	if !ok || value == nil {
		return ""
	}
	if text, isText := value.(string); isText {
		return strings.TrimSpace(text)
	}
	return ""
}

// List 把逗号分隔字符串或数组归一成字符串切片。
// 空结果返回 nil，让调用方能区分「没配置」与「配了一个空列表」。
func List(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return SplitList(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := String(item, ""); text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []string:
		if len(typed) == 0 {
			return nil
		}
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

// SplitList 把逗号分隔的字符串切成去空的切片；全空时返回 nil。
func SplitList(value string) []string {
	out := make([]string, 0, 4)
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Hours 解析每日调度时刻，支持数组与 "9,21" / "9 21" / "9;21" 这类写法。
// 越界值被丢弃，重复值去重，结果按升序排列；没有任何可用值时返回 fallback。
//
// 分隔符不能只认逗号：手写配置时 "9 21" 同样常见，只按逗号切会把整串当成
// 一个数字解析失败，静默地只留下第一个时刻。数组元素也不能只认 int——
// YAML 里写成 ["9", "21"] 时元素是 string，直接丢掉会让整个列表回落默认值。
func Hours(value any, fallback []int) []int {
	var raw []int
	switch typed := value.(type) {
	case nil:
		return fallback
	case []any:
		for _, item := range typed {
			if hour := Int(item, -1); hour >= 0 {
				raw = append(raw, hour)
			}
		}
	case []int:
		raw = append(raw, typed...)
	case string:
		fields := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == ' ' || r == ';' || r == '\t' || r == '\n'
		})
		for _, part := range fields {
			if hour, errAtoi := strconv.Atoi(strings.TrimSpace(part)); errAtoi == nil {
				raw = append(raw, hour)
			}
		}
	}
	seen := make(map[int]bool, len(raw))
	out := make([]int, 0, len(raw))
	for _, hour := range raw {
		if hour < 0 || hour > 23 || seen[hour] {
			continue
		}
		seen[hour] = true
		out = append(out, hour)
	}
	if len(out) == 0 {
		return fallback
	}
	sort.Ints(out)
	return out
}

// SelectionEntry 是模型显式注册清单的一行。
//
// Upstream 是上游目录里的原生 id，逐字节保留——上游对模型名大小写敏感，
// 改一个字母就会得到「model is unknown」。Name 是网关注册、客户端请求时用的
// 名字；留空时由各插件回落到自己派生的注册名。
type SelectionEntry struct {
	Upstream string
	Name     string
}

// Selection 把面板写下的模型勾选清单归一成 SelectionEntry 切片。
//
// nil 与空切片的语义必须分开：nil 表示「从未配置」（注册全部模型），
// 空切片表示「操作员把勾全取消了」（一个都不注册）。合并这两种情况会让
// 误清一次勾选导致整个 provider 静默消失。
//
// 按 Upstream 去重：同一模型填两行时下游会建两张同名索引，后写入的悄悄
// 覆盖前一条，客户端用自定义注册名反解时会指到错误的上游 id。
func Selection(value any) []SelectionEntry {
	if value == nil {
		return nil
	}
	items, okItems := value.([]any)
	if !okItems {
		if typed, isTyped := value.([]SelectionEntry); isTyped {
			return typed
		}
		return nil
	}
	out := make([]SelectionEntry, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		record, okRecord := item.(map[string]any)
		if !okRecord {
			continue
		}
		upstream := MapString(record, "upstream")
		if upstream == "" || seen[upstream] {
			continue
		}
		seen[upstream] = true
		out = append(out, SelectionEntry{
			Upstream: upstream,
			Name:     MapString(record, "name"),
		})
	}
	return out
}
