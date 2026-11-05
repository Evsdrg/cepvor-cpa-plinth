package schedule

import (
	"testing"
	"time"
)

// 正常返回必须透传，否则「取消信号」与「没有可用小时」这两条正常退出路径
// 会被吞掉，循环永不停止。
func TestGuardPassesThroughNormalResult(t *testing.T) {
	cancel := make(chan struct{})

	if got := Guard(cancel, func() bool { return true }, nil); !got {
		t.Fatalf("Guard must pass through true")
	}
	if got := Guard(cancel, func() bool { return false }, nil); got {
		t.Fatalf("Guard must pass through false so the loop can stop")
	}
}

// 单轮 panic 之后循环必须继续。这是本函数存在的全部理由：循环停止意味着
// 之后所有账号、所有日期都不再调度，且没有日志。
func TestGuardKeepsLoopAliveAfterPanic(t *testing.T) {
	cancel := make(chan struct{})
	close(cancel) // 让 panic 后的停顿立即返回

	var reported any
	got := Guard(cancel, func() bool {
		panic("iteration blew up")
	}, func(recovered any) { reported = recovered })

	if !got {
		t.Fatalf("Guard must report keepGoing=true after a panic")
	}
	if reported != "iteration blew up" {
		t.Fatalf("onPanic received %v, want the panic value", reported)
	}
}

// onPanic 为 nil 时不能因为回调缺失而把 panic 漏出去。
func TestGuardToleratesNilPanicCallback(t *testing.T) {
	cancel := make(chan struct{})
	close(cancel)

	got := Guard(cancel, func() bool { panic("boom") }, nil)
	if !got {
		t.Fatalf("Guard must keep the loop alive even without a callback")
	}
}

// panic 后的停顿必须可被取消信号打断，否则 Stop 会让关闭流程等满一分钟。
func TestGuardSettleDelayIsInterruptible(t *testing.T) {
	cancel := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(cancel)
	}()

	started := time.Now()
	Guard(cancel, func() bool { panic("boom") }, nil)
	elapsed := time.Since(started)

	if elapsed >= PanicSettleDelay {
		t.Fatalf("Guard waited %v; the settle delay must be interruptible by cancel", elapsed)
	}
}

func TestSleepOrCancelBasics(t *testing.T) {
	cancel := make(chan struct{})

	if !SleepOrCancel(cancel, 0) {
		t.Fatalf("a non-positive duration must return immediately with true")
	}
	if !SleepOrCancel(cancel, 10*time.Millisecond) {
		t.Fatalf("timing out must return true")
	}

	closed := make(chan struct{})
	close(closed)
	if SleepOrCancel(closed, time.Second) {
		t.Fatalf("a closed channel must return false without waiting")
	}
}

// panic 连续发生时必须每次都返回 true，否则循环会在第二次 panic 后退出。
func TestGuardSurvivesRepeatedPanics(t *testing.T) {
	cancel := make(chan struct{})
	close(cancel)

	count := 0
	for i := 0; i < 5; i++ {
		if !Guard(cancel, func() bool { panic("repeated") }, func(any) { count++ }) {
			t.Fatalf("iteration %d stopped the loop", i)
		}
	}
	if count != 5 {
		t.Fatalf("onPanic called %d times, want 5", count)
	}
}
