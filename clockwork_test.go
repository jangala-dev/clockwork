package clockwork

import (
	"reflect"
	"testing"
	"time"
)

func TestFakeClockAfter(t *testing.T) {
	t.Parallel()
	fc := &fakeClock{}

	neg := fc.After(-1)
	select {
	case <-neg:
	default:
		t.Errorf("negative did not return!")
	}

	zero := fc.After(0)
	select {
	case <-zero:
	default:
		t.Errorf("zero did not return!")
	}
	one := fc.After(1)
	two := fc.After(2)
	six := fc.After(6)
	ten := fc.After(10)
	fc.Advance(1)
	select {
	case <-one:
	default:
		t.Errorf("one did not return!")
	}
	select {
	case <-two:
		t.Errorf("two returned prematurely!")
	case <-six:
		t.Errorf("six returned prematurely!")
	case <-ten:
		t.Errorf("ten returned prematurely!")
	default:
	}
	fc.Advance(1)
	select {
	case <-two:
	default:
		t.Errorf("two did not return!")
	}
	select {
	case <-six:
		t.Errorf("six returned prematurely!")
	case <-ten:
		t.Errorf("ten returned prematurely!")
	default:
	}
	fc.Advance(1)
	select {
	case <-six:
		t.Errorf("six returned prematurely!")
	case <-ten:
		t.Errorf("ten returned prematurely!")
	default:
	}
	fc.Advance(3)
	select {
	case <-six:
	default:
		t.Errorf("six did not return!")
	}
	select {
	case <-ten:
		t.Errorf("ten returned prematurely!")
	default:
	}
	fc.Advance(100)
	select {
	case <-ten:
	default:
		t.Errorf("ten did not return!")
	}
}

func TestNotifyBlockers(t *testing.T) {
	t.Parallel()
	b1 := &blocker{1, make(chan struct{})}
	b2 := &blocker{2, make(chan struct{})}
	b3 := &blocker{5, make(chan struct{})}
	b4 := &blocker{10, make(chan struct{})}
	b5 := &blocker{10, make(chan struct{})}
	bs := []*blocker{b1, b2, b3, b4, b5}
	bs1 := notifyBlockers(bs, 2)
	if n := len(bs1); n != 4 {
		t.Fatalf("got %d blockers, want %d", n, 4)
	}
	select {
	case <-b2.ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for channel close!")
	}
	bs2 := notifyBlockers(bs1, 10)
	if n := len(bs2); n != 2 {
		t.Fatalf("got %d blockers, want %d", n, 2)
	}
	select {
	case <-b4.ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for channel close!")
	}
	select {
	case <-b5.ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for channel close!")
	}
}

func TestNewFakeClock(t *testing.T) {
	t.Parallel()
	fc := NewFakeClock()
	now := fc.Now()
	if now.IsZero() {
		t.Fatalf("fakeClock.Now() fulfills IsZero")
	}

	now2 := fc.Now()
	if !reflect.DeepEqual(now, now2) {
		t.Fatalf("fakeClock.Now() returned different value: want=%#v got=%#v", now, now2)
	}
}

func TestNewFakeClockAt(t *testing.T) {
	t.Parallel()
	t1 := time.Date(1999, time.February, 3, 4, 5, 6, 7, time.UTC)
	fc := NewFakeClockAt(t1)
	now := fc.Now()
	if !reflect.DeepEqual(now, t1) {
		t.Fatalf("fakeClock.Now() returned unexpected non-initialised value: want=%#v, got %#v", t1, now)
	}
}

func TestFakeClockSince(t *testing.T) {
	t.Parallel()
	fc := NewFakeClock()
	now := fc.Now()
	elapsedTime := time.Second
	fc.Advance(elapsedTime)
	if fc.Since(now) != elapsedTime {
		t.Fatalf("fakeClock.Since() returned unexpected duration, got: %d, want: %d", fc.Since(now), elapsedTime)
	}
}

func TestFakeClockTimers(t *testing.T) {
	t.Parallel()
	fc := &fakeClock{}

	zero := fc.NewTimer(0)

	if zero.Stop() {
		t.Errorf("zero timer could be stopped")
	}
	select {
	case <-zero.C():
	default:
		t.Errorf("zero timer didn't emit time")
	}

	one := fc.NewTimer(1)

	select {
	case <-one.C():
		t.Errorf("non-zero timer did emit time")
	default:
	}
	if !one.Stop() {
		t.Errorf("non-zero timer couldn't be stopped")
	}

	fc.Advance(5)

	select {
	case <-one.C():
		t.Errorf("stopped timer did emit time")
	default:
	}

	if one.Reset(1) {
		t.Errorf("resetting stopped timer didn't return false")
	}
	if !one.Reset(1) {
		t.Errorf("resetting active timer didn't return true")
	}

	fc.Advance(1)

	if one.Stop() {
		t.Errorf("triggered timer could be stopped")
	}
	select {
	case <-one.C():
	default:
		t.Errorf("triggered timer didn't emit time")
	}

	fc.Advance(1)

	select {
	case <-one.C():
		t.Errorf("triggered timer emitted time more than once")
	default:
	}

	one.Reset(0)

	if one.Stop() {
		t.Errorf("reset to zero timer could be stopped")
	}
	select {
	case <-one.C():
	default:
		t.Errorf("reset to zero timer didn't emit time")
	}
}

// withTimeout checks that the test finished executing within a certain time.
// If it runs over time, the test will be failed immediately.
// This is not an accurate timer, it's just used to fail deadlocking tests.
func withTimeout(t *testing.T, d time.Duration, fn func()) {
	step := make(chan struct{})
	go func() {
		step <- struct{}{}
		fn()
		step <- struct{}{}
	}()
	<-step // Wait for start
	select {
	case <-step: // Wait for finish
	case <-time.After(d):
		t.Fatalf("timed out")
	}
}

func TestBlockingOnTimers(t *testing.T) {
	t.Parallel()
	withTimeout(t, 100*time.Millisecond, func() {
		fc := &fakeClock{}

		fc.NewTimer(0)
		fc.BlockUntil(0)

		one := fc.NewTimer(1)
		fc.BlockUntil(1)

		one.Stop()
		fc.BlockUntil(0)

		one.Reset(1)
		fc.BlockUntil(1)

		_ = fc.NewTimer(2)
		_ = fc.NewTimer(3)
		fc.BlockUntil(3)

		one.Stop()
		fc.BlockUntil(2)

		fc.Advance(3)
		fc.BlockUntil(0)
	})
}

func TestAdvancePastAfter(t *testing.T) {
	t.Parallel()
	fc := &fakeClock{}

	start := fc.Now()
	one := fc.After(1)
	two := fc.After(2)
	six := fc.After(6)

	fc.Advance(1)
	if start.Add(1).Sub(<-one) > 0 {
		t.Errorf("timestamp is too early")
	}

	fc.Advance(5)
	if start.Add(2).Sub(<-two) > 0 {
		t.Errorf("timestamp is too early")
	}
	if start.Add(6).Sub(<-six) > 0 {
		t.Errorf("timestamp is too early")
	}
}

// TestFakeClockRace detects data races in fakeClock when invoked with run using `go -race ...`.
// There are no failure conditions when invoked without the -race flag.
func TestFakeClockRace(t *testing.T) {
	t.Parallel()
	fc := &fakeClock{}
	d := time.Second
	go func() { fc.Advance(d) }()
	go func() { fc.NewTicker(d) }()
	go func() { fc.NewTimer(d) }()
	go func() { fc.Sleep(d) }()
}

func TestSet(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name              string
		start             time.Time
		now               time.Time
		sleepers          []time.Duration
		wantNotifications int
	}{
		{
			name:  "Year 10k",
			start: time.Now(),
			now:   time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "Back to 2k",
			start: time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC),
			now:   time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "Leap forward",
			start: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			now:   time.Date(2000, 1, 1, 1, 0, 0, 0, time.UTC),
			sleepers: []time.Duration{
				time.Second,
				time.Hour,
				2 * time.Hour,
			},
			wantNotifications: 2,
		},
		{
			name:  "Leap backwards",
			start: time.Date(2000, 1, 1, 1, 0, 0, 0, time.UTC),
			now:   time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			sleepers: []time.Duration{
				time.Second,
				time.Hour,
				2 * time.Hour,
			},
			wantNotifications: 0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fc := NewFakeClockAt(test.start)

			var sleepers []<-chan time.Time
			for _, s := range test.sleepers {
				sleepers = append(sleepers, fc.After(s))
			}

			fc.Set(test.now)
			if fc.Now() != test.now {
				t.Errorf("failed to Set(): got %v, want %v", fc.Now(), test.now)
			}

			gotNotifications := 0
			for _, s := range sleepers {
				select {
				case wakeTime := <-s:
					t.Logf("%v woke up", wakeTime)
					gotNotifications++
				default:
					t.Log("A sleeper is still sleeping")
				}
			}
			if gotNotifications != test.wantNotifications {
				t.Errorf("got incorrect number of notifications: got %d, want %d", gotNotifications, test.wantNotifications)
			}

		})
	}
}
