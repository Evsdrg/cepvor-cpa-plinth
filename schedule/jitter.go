// Package schedule 提供自研插件共用的定时调度原语。
//
// 这里的每个函数都只做一件事，且都是纯函数：给定同样的输入永远得到同样的
// 输出。签到这类"看起来像自然使用"的调度必须可重放——进程重启后同一账号
// 同一天不能因为重新随机而换到另一个时刻，否则会把已经签过的账号再签一遍。
package schedule

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"time"
)

// JitterWindow 是签到抖动窗口的一半：±10 分钟。
const JitterWindow = 10 * time.Minute

// AccountSlot 是一个账号在某个配置小时上的实际签到时刻。
type AccountSlot struct {
	// AccountName 是参与派生抖动的账号标识，通常是宿主给的文件名或 auth id。
	// 它只用于派生，不会出现在日志以外的地方。
	AccountName string
	// Hour 是配置里写的小时（本地时间），例如 9 表示用户想要早上 9 点。
	Hour int
	// At 是加上抖动后的实际触发时刻。
	At time.Time
}

// Jitter 返回某个账号在某天某个小时上的确定性抖动，范围 [-10min, +10min]，
// 精度到秒。
//
// 三个输入共同决定抖动，缺一不可：
//   - accountName：不同账号必须落在不同时刻，否则同一秒打多个账号的接口，
//     在上游看来是明显的脚本特征；
//   - day：同一账号不同日期必须不同，否则每天的签到时刻都是一个固定偏移，
//     同样可被识别；
//   - hour：同一账号在同一天里若有多个签到小时，各小时的抖动也应不同，
//     否则多个时段会被识别成同一个模式。
//
// 用 SHA-256 而不是 math/rand：需要跨进程重启稳定。rand 的种子取自时间就会
// 在每次重启后改变结果，导致重复签到。
func Jitter(accountName string, day time.Time, hour int) time.Duration {
	seed := fmt.Sprintf("%s|%s|%d", accountName, day.Format("2006-01-02"), hour)
	sum := sha256.Sum256([]byte(seed))
	// 取前 4 字节映射到 [-600, +600] 秒（含端点共 1201 个取值）。
	// 用取模而不是浮点，避免不同平台上的浮点差异。
	span := int64(2*JitterWindow/time.Second) + 1
	offset := int64(binary.BigEndian.Uint32(sum[:4]))%span - int64(JitterWindow/time.Second)
	return time.Duration(offset) * time.Second
}

// NextSlots 计算每个账号在给定小时表下的下一个触发时刻，按时间升序返回。
//
// 语义要点：
//   - hours 是本地墙钟小时。用户写 "9" 指的是他自己的早上 9 点，所以用
//     now.Location() 而不是固定 UTC 偏移；
//   - 每个 (账号, 小时) 组合独立计算抖动，因此不同账号不会同时触发；
//   - 已经过去的时刻会顺延到次日同一小时（抖动按次日重新派生，所以次日
//     相对今天的触发时刻也不同）；
//   - 返回空切片表示没有可用的小时或没有账号。
func NextSlots(now time.Time, accountNames []string, hours []int) []AccountSlot {
	if len(accountNames) == 0 || len(hours) == 0 {
		return nil
	}
	slots := make([]AccountSlot, 0, len(accountNames)*len(hours))
	for _, name := range accountNames {
		for _, hour := range hours {
			if hour < 0 || hour > 23 {
				continue
			}
			at := slotFor(name, now, hour)
			slots = append(slots, AccountSlot{AccountName: name, Hour: hour, At: at})
		}
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].At.Equal(slots[j].At) {
			// 同一时刻时按账号名稳定排序，保证测试可复现。
			return slots[i].AccountName < slots[j].AccountName
		}
		return slots[i].At.Before(slots[j].At)
	})
	resolveSlotCollisions(slots)
	return slots
}

// DaySlots 返回 now 所在本地日内全天的槽位，包含已经过去的时刻。
//
// NextSlots 回答的是「下一个未来槽位是什么」；本函数回答的是「今天原本排了
// 哪些」。二者用途不同：补签必须看得见今天已经错过的槽位，否则进程在槽位之后
// 才启动（或前一个账号耗时过长越过后续槽位）时，那些工作会被静默跳过，一直等
// 到第二天。
func DaySlots(now time.Time, accountNames []string, hours []int) []AccountSlot {
	if len(accountNames) == 0 || len(hours) == 0 {
		return nil
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	slots := make([]AccountSlot, 0, len(accountNames)*len(hours))
	for _, name := range accountNames {
		for _, hour := range hours {
			if hour < 0 || hour > 23 {
				continue
			}
			at := day.Add(time.Duration(hour) * time.Hour).Add(Jitter(name, day, hour))
			slots = append(slots, AccountSlot{AccountName: name, Hour: hour, At: at})
		}
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].At.Equal(slots[j].At) {
			return slots[i].AccountName < slots[j].AccountName
		}
		return slots[i].At.Before(slots[j].At)
	})
	resolveSlotCollisions(slots)
	return slots
}

// resolveSlotCollisions 把撞在同一秒的槽位依次后移一秒。
//
// 抖动的取值空间只有 1201 个，账号一多必然碰撞（生日悖论：40 个账号撞一次的
// 概率约 67%）。碰撞意味着两个账号在同一秒打同一个接口——正是抖动要消除的脚本
// 特征。账号数远小于窗口秒数，所以不会推出 ±10 分钟之外。
func resolveSlotCollisions(slots []AccountSlot) {
	for i := 1; i < len(slots); i++ {
		if slots[i].At.Equal(slots[i-1].At) {
			slots[i].At = slots[i-1].At.Add(time.Second)
		}
	}
}

// slotFor 返回某个账号某个小时的下一个触发时刻，必要时顺延到次日。
func slotFor(accountName string, now time.Time, hour int) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for attempt := 0; attempt < 3; attempt++ {
		base := day.AddDate(0, 0, attempt)
		at := base.Add(time.Duration(hour) * time.Hour).Add(Jitter(accountName, base, hour))
		if at.After(now) {
			return at
		}
	}
	return day.AddDate(0, 0, 3).Add(time.Duration(hour) * time.Hour)
}

// NextAt 是 NextSlots 的便捷形式：返回最早的一个槽位。
func NextAt(now time.Time, accountNames []string, hours []int) (AccountSlot, bool) {
	slots := NextSlots(now, accountNames, hours)
	if len(slots) == 0 {
		return AccountSlot{}, false
	}
	return slots[0], true
}
