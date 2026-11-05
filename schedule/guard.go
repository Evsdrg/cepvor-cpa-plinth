// Package schedule 提供自研插件共用的定时调度原语。
//
// 这里的每个函数都只做一件事，且都是纯函数：给定同样的输入永远得到同样的
// 输出。签到这类"看起来像自然使用"的调度必须可重放——进程重启后同一账号
// 同一天不能因为重新随机而换到另一个时刻，否则会把已经签过的账号再签一遍。
//
// 唯一的例外是 Guard：它不产生调度结果，只保证一个长期存活的循环不会被单次
// 迭代里的 panic 带走。它放在这里是因为三个签到插件都需要同一个行为，而
// 写三遍最容易在 recover 的边界上写错。
package schedule

import (
	"time"
)

// PanicSettleDelay 是循环被单轮 panic 打断后的暂停时长。
//
// 停一下再继续的理由：如果 panic 是持续性的（例如某个账号的凭据结构每次都会
// 触发同一处越界），紧接下一轮会立刻再炸，日志被刷屏且看不出发生的次数。
// 一分钟的间隔让「异常发生了多少次、每次隔多久」在日志里可读。
const PanicSettleDelay = time.Minute

// SleepOrCancel 睡到 d 之后返回 true；收到取消信号立刻返回 false。
// d <= 0 时立即返回 true（不占用一个定时器）。
func SleepOrCancel(cancel <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-cancel:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-cancel:
		return false
	case <-timer.C:
		return true
	}
}

// Guard 执行 body 并捕获 panic，把一次迭代的崩溃与循环的存亡分开。
//
// 语义：
//   - body 正常返回时，透传它的返回值（false 表示循环应当结束）；
//   - body panic 时，用 onPanic 报告 panic 值，暂停 PanicSettleDelay（可被
//     cancel 打断），随后返回 true —— 循环继续，因为停止意味着之后所有
//     账号、所有日期都不再调度；
//   - cancel 已关闭时不额外等待。
//
// onPanic 可以为 nil；那样 panic 被静默吞掉，只保留「循环继续」这一个效果
// （测试里用得上）。
func Guard(cancel <-chan struct{}, body func() bool, onPanic func(recovered any)) (keepGoing bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if onPanic != nil {
				onPanic(recovered)
			}
			SleepOrCancel(cancel, PanicSettleDelay)
			keepGoing = true
		}
	}()
	return body()
}
