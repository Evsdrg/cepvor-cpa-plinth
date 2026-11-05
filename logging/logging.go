// Package logging 是自研插件共用的日志入口。
//
// 为什么不直接调 abi.Host.Log：
//
//  1. 宿主的 console formatter 只打印 logFieldOrder 白名单里的键
//     （internal/logging/global_logger.go:54-62）。白名单外的字段会被静默
//     丢弃——日志看起来打了，实际什么都不显示。散落各处的
//     map[string]any 很容易写进这样一个键名；这里的构造器把可选键名收敛
//     成一小组常量，用错键名没有对应的构造器可用。
//
//  2. 插件的 message 需要统一形态 `<插件>.<模块>：<中文描述>`，让操作员在
//     混合日志里能按组件过滤。标识符、路径、模型名保持原文，只翻译描述。
//
//  3. 级别语义固定：info 是正常的状态变化（签到成功、凭据续期、目录更新），
//     warn 是可恢复的降级（单个账号失败但整体继续、回退到内置列表），
//     error 是需要人工介入（凭据死亡、后台循环崩溃）。
//
// 这一层不持有包级状态。并发场景（failover 同时遍历多个账号）里每个调用点
// 显式传入自己那个账号的字段，不会出现日志串号。
package logging

import (
	"github.com/Evsdrg/cepvor-cpa-plinth/abi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// 级别取值。宿主的 callHostLog 按 level 字符串分派，未识别的值落到 debug，
// 所以这里只用它认识的三个（另有 trace，插件侧不需要）。
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// 字段键名。全部取自宿主的 logFieldOrder 白名单；改动前先核对那份列表，
// 否则字段会消失而不是报错。
const (
	keyProvider  = "provider"
	keyModel     = "model"
	keyAuthID    = "auth_id"
	keyAuthIndex = "auth_index"
	keyError     = "error"
	keyReason    = "reason"
	keyState     = "state"
	keyOperation = "operation"
)

// Field 是一条结构化日志字段。调用方不直接构造它，而用下面的构造器，
// 以保证键名在白名单内。
type Field struct {
	Key   string
	Value string
}

// Provider 标识这条日志属于哪个提供方（插件 id，如 codearts、qoder）。
func Provider(value string) Field { return Field{Key: keyProvider, Value: value} }

// Model 标识涉及的模型名（保持上游原文大小写）。
func Model(value string) Field { return Field{Key: keyModel, Value: value} }

// Auth 标识涉及的账号。用 auth 文件名或 uid 均可，但同一插件内应保持一致，
// 否则日志里同一个账号会出现两种写法。
func Auth(value string) Field { return Field{Key: keyAuthID, Value: value} }

// AuthIndex 标识宿主的运行期账号索引。它跨重启会变，只用于和宿主日志对齐。
func AuthIndex(value string) Field { return Field{Key: keyAuthIndex, Value: value} }

// Err 记录失败原因。nil 返回空 Field，会被 emit 跳过，所以调用点可以无条件
// 写 Err(err) 而不必先判空。
func Err(err error) Field {
	if err == nil {
		return Field{}
	}
	return Field{Key: keyError, Value: err.Error()}
}

// Reason 记录一个判定依据，用于解释为什么走了这条分支。
func Reason(value string) Field { return Field{Key: keyReason, Value: value} }

// State 记录一个状态取值（如 claimed、already-claimed、cooled）。
func State(value string) Field { return Field{Key: keyState, Value: value} }

// Operation 记录正在执行的操作名（如 checkin、refresh、catalog）。
func Operation(value string) Field { return Field{Key: keyOperation, Value: value} }

// Info 记录正常的状态变化。
func Info(h *abi.Host, message string, fields ...Field) {
	emit(h, LevelInfo, message, fields)
}

// Warn 记录可恢复的降级：某个账号、某个模型或某次刷新失败，但整体仍在服务。
func Warn(h *abi.Host, message string, fields ...Field) {
	emit(h, LevelWarn, message, fields)
}

// Error 记录需要人工介入的失败：凭据死亡、后台循环停止、账号全部不可用。
func Error(h *abi.Host, message string, fields ...Field) {
	emit(h, LevelError, message, fields)
}

// Emit 是三个快捷函数的一般形式，用于级别在运行时才确定的场景
// （例如一条日志同时承载「已领取」与「领取失败」两种结果）。
// level 必须是 Info/Warn/Error 之一；其他值会被宿主归入 debug，
// 在默认级别下看不到。
func Emit(h *abi.Host, level, message string, fields ...Field) {
	emit(h, level, message, fields)
}

// emit 组装一次 host.log 调用。
//
// h 可以为 nil：后台循环在自己的 goroutine 里没有宿主回调时用它打日志，
// 此时不带 host_callback_id，宿主也就无法补上 request_id——这是刻意的取舍，
// 循环日志本来就不属于某一次客户端请求。
func emit(h *abi.Host, level, message string, fields []Field) {
	payload := map[string]any{
		"level":   level,
		"message": message,
	}
	if h != nil && h.CallbackID != "" {
		payload["host_callback_id"] = h.CallbackID
	}
	collected := make(map[string]any, len(fields))
	for _, field := range fields {
		if field.Key == "" || field.Value == "" {
			continue
		}
		collected[field.Key] = field.Value
	}
	if len(collected) > 0 {
		payload["fields"] = collected
	}
	_, _ = abi.HostCall(pluginabi.MethodHostLog, payload)
}
