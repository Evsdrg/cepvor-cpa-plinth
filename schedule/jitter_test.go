package schedule

import (
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

// 抖动必须落在 ±10 分钟之内，否则用户配置的 9 点可能变成 8:49 或 9:11。
func TestJitterStaysWithinWindow(t *testing.T) {
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 500; i++ {
		name := "account-" + string(rune('a'+i%26)) + string(rune('0'+i/26%10))
		j := Jitter(name, day, 9)
		if j < -JitterWindow || j > JitterWindow {
			t.Fatalf("%s: jitter %v outside ±%v", name, j, JitterWindow)
		}
	}
}

// 同一天同一账号同一小时必须稳定：进程重启后不能换时刻，否则会把已签过的
// 账号再签一遍。
func TestJitterIsStableAcrossCalls(t *testing.T) {
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	first := Jitter("acct-1", day, 9)
	for i := 0; i < 100; i++ {
		if got := Jitter("acct-1", day, 9); got != first {
			t.Fatalf("call %d returned %v, want stable %v", i, got, first)
		}
	}
}

// 抖动本身只有 1201 个取值，40 个账号必然会有碰撞（生日悖论）。这里只
// 要求分布足够分散；真正的"不同账号不撞同一秒"由 NextSlots 的冲突消解保证。
func TestJitterSpreadsAcrossAccounts(t *testing.T) {
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	seen := map[time.Duration]bool{}
	for i := 0; i < 40; i++ {
		name := "account-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		seen[Jitter(name, day, 9)] = true
	}
	if len(seen) < 30 {
		t.Fatalf("only %d distinct jitters across 40 accounts; the spread is too narrow", len(seen))
	}
}

// 冲突消解之后，任何两个账号都不得落在同一时刻——同一秒打多个账号的接口
// 是抖动要消除的脚本特征。
func TestNextSlotsResolvesCollisions(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	names := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		names = append(names, "account-"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	slots := NextSlots(now, names, []int{9})
	seen := map[time.Time]string{}
	for _, slot := range slots {
		if prev, ok := seen[slot.At]; ok {
			t.Fatalf("%s and %s both scheduled at %v", prev, slot.AccountName, slot.At)
		}
		seen[slot.At] = slot.AccountName
	}
}

// 同一账号不同日期必须不同，否则每天都是固定偏移，同样可被识别。
func TestJitterDiffersAcrossDays(t *testing.T) {
	names := []string{"acct-1", "acct-2", "acct-3"}
	sameCount := 0
	for d := 0; d < 30; d++ {
		day := time.Date(2026, 10, 1+d, 0, 0, 0, 0, time.UTC)
		next := day.AddDate(0, 0, 1)
		for _, name := range names {
			if Jitter(name, day, 9) == Jitter(name, next, 9) {
				sameCount++
			}
		}
	}
	// 30 天里 3 个账号相邻两天抖动全同的概率极低；允许少量碰撞但不应普遍。
	if sameCount > 3 {
		t.Fatalf("%d/90 adjacent days shared a jitter; the offset is effectively fixed", sameCount)
	}
}

// 同一账号同一天不同小时也应不同。
func TestJitterDiffersAcrossHours(t *testing.T) {
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	if Jitter("acct-1", day, 9) == Jitter("acct-1", day, 21) {
		t.Fatal("9:00 and 21:00 share a jitter; multi-slot schedules would look like one pattern")
	}
}

// 抖动必须带秒级精度，不能只落在整分钟上。
func TestJitterHasSubMinutePrecision(t *testing.T) {
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	nonMinute := 0
	for i := 0; i < 100; i++ {
		name := "account-" + string(rune('a'+i%26)) + string(rune('0'+i/26%10))
		if Jitter(name, day, 9)%time.Minute != 0 {
			nonMinute++
		}
	}
	if nonMinute < 80 {
		t.Fatalf("only %d/100 jitters have sub-minute precision", nonMinute)
	}
}

// 时区必须跟随本地墙钟：用户写 9 点指的是他自己的早上 9 点。
func TestNextSlotsFollowsLocalWallClock(t *testing.T) {
	loc := mustLoad(t, "Asia/Shanghai")
	now := time.Date(2026, 10, 19, 8, 0, 0, 0, loc)
	slots := NextSlots(now, []string{"acct-1"}, []int{9})
	if len(slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(slots))
	}
	// 9:00 ± 10min 仍然在 8:50–9:10 之间，必须大于 now。
	if !slots[0].At.After(now) {
		t.Fatalf("slot %v is not after now %v", slots[0].At, now)
	}
	if slots[0].At.Hour() != 9 && slots[0].At.Hour() != 8 {
		t.Fatalf("slot hour = %d, want 8 or 9", slots[0].At.Hour())
	}
}

// 已经过去的时段必须顺延到次日，而不是返回过去时刻（会立即触发）。
func TestNextSlotsSkipsPastSlots(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 10, 19, 23, 0, 0, 0, loc)
	slots := NextSlots(now, []string{"acct-1"}, []int{9})
	if len(slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(slots))
	}
	if !slots[0].At.After(now) {
		t.Fatalf("slot %v is in the past", slots[0].At)
	}
	if slots[0].At.Day() != 20 {
		t.Fatalf("slot day = %d, want 20 (tomorrow)", slots[0].At.Day())
	}
}

// 多账号多时段：结果按时间升序，且相邻槽位不应大量撞在同一秒。
func TestNextSlotsOrdersAndSpreads(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	names := []string{"a", "b", "c", "d", "e"}
	slots := NextSlots(now, names, []int{9, 21})
	if len(slots) != len(names)*2 {
		t.Fatalf("slots = %d, want %d", len(slots), len(names)*2)
	}
	for i := 1; i < len(slots); i++ {
		if slots[i].At.Before(slots[i-1].At) {
			t.Fatalf("slot %d (%v) before slot %d (%v)", i, slots[i].At, i-1, slots[i-1].At)
		}
	}
	seen := map[time.Time]bool{}
	for _, slot := range slots {
		if seen[slot.At] {
			t.Fatalf("duplicate slot time %v", slot.At)
		}
		seen[slot.At] = true
	}
}

// 空输入必须返回空，不能 panic。
func TestNextSlotsEmptyInputs(t *testing.T) {
	now := time.Now()
	if got := NextSlots(now, nil, []int{9}); len(got) != 0 {
		t.Fatalf("no accounts should yield no slots, got %d", len(got))
	}
	if got := NextSlots(now, []string{"a"}, nil); len(got) != 0 {
		t.Fatalf("no hours should yield no slots, got %d", len(got))
	}
}

// 非法小时必须被忽略，而不是算出奇怪的时间。
func TestNextSlotsIgnoresInvalidHours(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	slots := NextSlots(now, []string{"a"}, []int{-1, 24, 25})
	if len(slots) != 0 {
		t.Fatalf("invalid hours produced %d slots", len(slots))
	}
}

// TestDaySlotsKeepsPastSlots pins the difference between NextSlots and DaySlots.
//
// NextSlots answers "what is the next future slot"; DaySlots answers "what was
// scheduled for today, including what already passed". Catch-up scheduling needs
// the latter: when the process starts after a slot, or a slow check-in runs past
// the following slots, those slots must still be visible or the day is lost.
func TestDaySlotsKeepsPastSlots(t *testing.T) {
	loc := mustLoad(t, "Asia/Shanghai")
	names := []string{"acct-a"}
	hours := []int{9}

	at8 := time.Date(2026, 10, 23, 8, 0, 0, 0, loc)
	at10 := time.Date(2026, 10, 23, 10, 0, 0, 0, loc)

	next, _ := NextAt(at10, names, hours)
	if next.At.Day() != 24 {
		t.Fatalf("NextAt from 10:00 = %v, want tomorrow; this test assumes the old semantics", next.At)
	}

	today := DaySlots(at10, names, hours)
	if len(today) != 1 {
		t.Fatalf("DaySlots returned %d entries, want 1", len(today))
	}
	if today[0].At.Day() != 23 {
		t.Fatalf("DaySlots slot = %v, want today (23); a passed slot must remain visible for catch-up", today[0].At)
	}
	if today[0].At.Hour() != 9 || !today[0].At.Before(at10) {
		t.Fatalf("DaySlots slot = %v, want the already-passed 09:xx slot", today[0].At.Format("15:04:05"))
	}
	// Same instant as NextSlots would compute for that day.
	at8slots := NextSlots(at8, names, hours)
	if len(at8slots) != 1 || !at8slots[0].At.Equal(today[0].At) {
		t.Fatalf("DaySlots = %v, NextSlots(08:00) = %v; both must describe the same scheduled instant",
			today[0].At, at8slots)
	}
}

// TestDaySlotsEmptyInputs keeps the guard rails of the sibling helper.
func TestDaySlotsEmptyInputs(t *testing.T) {
	loc := mustLoad(t, "Asia/Shanghai")
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, loc)
	if got := DaySlots(now, nil, []int{9}); len(got) != 0 {
		t.Fatalf("DaySlots with no accounts = %v, want empty", got)
	}
	if got := DaySlots(now, []string{"a"}, nil); len(got) != 0 {
		t.Fatalf("DaySlots with no hours = %v, want empty", got)
	}
	if got := DaySlots(now, []string{"a"}, []int{24, -1}); len(got) != 0 {
		t.Fatalf("DaySlots with out-of-range hours = %v, want empty", got)
	}
}
