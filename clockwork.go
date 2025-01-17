package clockwork

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Clock provides an interface that packages can use instead of directly
// using the time module, so that chronology-related behavior can be tested
type Clock interface {
	After(d time.Duration) <-chan time.Time
	Sleep(d time.Duration)
	Now() time.Time
	Since(t time.Time) time.Duration
	NewTicker(d time.Duration) Ticker
	NewTimer(d time.Duration) Timer
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer provides an interface to a time.Timer which is testable.
// See https://golang.org/pkg/time/#Timer for more details on how timers work.
type Timer interface {
	C() <-chan time.Time
	Reset(d time.Duration) bool
	Stop() bool

	T() *time.Timer // underlying *time.Timer (nil when using a FakeClock)
}

// FakeClock provides an interface for a clock which can be
// manually advanced through time
type FakeClock interface {
	Clock
	// Advance advances the FakeClock to a new point in time, ensuring any existing
	// sleepers are notified appropriately before returning.
	Advance(d time.Duration)
	// BlockUntil will block until the FakeClock has the given number of
	// sleepers (callers of Sleep or After).
	BlockUntil(n int)
	// Set sets the FakeClock to a new point in time, ensuring channels from any
	// existing sleepers (callers of Sleep or After) are notified appropriately
	// before returning.
	Set(t time.Time)
}

// NewRealClock returns a Clock which simply delegates calls to the actual time
// package; it should be used by packages in production.
func NewRealClock() Clock {
	return &realClock{}
}

// NewFakeClock returns a FakeClock implementation which can be
// manually advanced through time for testing. The initial time of the
// FakeClock will be an arbitrary non-zero time.
func NewFakeClock() FakeClock {
	// use a fixture that does not fulfill Time.IsZero()
	return NewFakeClockAt(time.Date(1984, time.April, 4, 0, 0, 0, 0, time.UTC))
}

// NewFakeClockAt returns a FakeClock initialised at the given time.Time.
func NewFakeClockAt(t time.Time) FakeClock {
	return &fakeClock{
		time: t,
	}
}

type realClock struct{}

func (rc *realClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (rc *realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

func (rc *realClock) Now() time.Time {
	return time.Now()
}

func (rc *realClock) Since(t time.Time) time.Duration {
	return rc.Now().Sub(t)
}

func (rc *realClock) NewTicker(d time.Duration) Ticker {
	return &realTicker{time.NewTicker(d)}
}

func (rc *realClock) NewTimer(d time.Duration) Timer {
	return &realTimer{time.NewTimer(d)}
}

func (rc *realClock) AfterFunc(d time.Duration, f func()) Timer {
	return &realTimer{time.AfterFunc(d, f)}
}

type realTimer struct {
	t *time.Timer
}

func (rt *realTimer) C() <-chan time.Time { return rt.t.C }

func (rt *realTimer) T() *time.Timer { return rt.t }

func (rt *realTimer) Reset(d time.Duration) bool {
	return rt.t.Reset(d)
}

func (rt *realTimer) Stop() bool {
	return rt.t.Stop()
}

type fakeClock struct {
	sleepers []*sleeper
	blockers []*blocker
	time     time.Time

	l sync.RWMutex
}

// sleeper represents a waiting timer from NewTimer, Sleep, After, etc.
type sleeper struct {
	until time.Time
	l     sync.RWMutex // Guards until

	callback func(interface{}, time.Time)
	arg      interface{}

	ch   chan time.Time
	done uint32
	fc   *fakeClock // needed for Reset()
}

// blocker represents a caller of BlockUntil
type blocker struct {
	count int
	ch    chan struct{}
}

func (s *sleeper) awaken(now time.Time) {
	if atomic.CompareAndSwapUint32(&s.done, 0, 1) {
		s.callback(s.arg, now)
	}
}

func (s *sleeper) C() <-chan time.Time { return s.ch }

func (s *sleeper) T() *time.Timer { return nil }

func (s *sleeper) Reset(d time.Duration) bool {
	active := s.Stop()
	s.until = s.fc.Now().Add(d)
	defer s.fc.addTimer(s)
	defer atomic.StoreUint32(&s.done, 0)
	return active
}

func (s *sleeper) Until() time.Time {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.until
}

func (s *sleeper) SetUntil(t time.Time) {
	s.l.Lock()
	defer s.l.Unlock()
	s.until = t
}

func (s *sleeper) Stop() bool {
	stopped := atomic.CompareAndSwapUint32(&s.done, 0, 1)
	if stopped {
		s.SetUntil(s.fc.Now()) // Expire the timer
		s.fc.Advance(0)        // Notify blockers
	}
	return stopped
}

// After mimics time.After; it waits for the given duration to elapse on the
// fakeClock, then sends the current time on the returned channel.
func (fc *fakeClock) After(d time.Duration) <-chan time.Time {
	return fc.NewTimer(d).C()
}

// NewTimer creates a new Timer that will send the current time on its channel
// after the given duration elapses on the fake clock.
func (fc *fakeClock) NewTimer(d time.Duration) Timer {
	done := make(chan time.Time, 1)
	s := &sleeper{
		fc: fc,
		// Use fc.Now() to ensure fc.l is held when accessing fc.time.
		until:    fc.Now().Add(d),
		callback: sendTime,
		arg:      done,
		ch:       done,
	}
	fc.addTimer(s)
	return s
}

// AfterFunc waits for the duration to elapse on the fake clock and then calls f
// in its own goroutine.
// It returns a Timer that can be used to cancel the call using its Stop method.
func (fc *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	s := &sleeper{
		fc: fc,
		// Use fc.Now() to ensure fc.l is held when accessing fc.time.
		until:    fc.Now().Add(d),
		callback: goFunc,
		arg:      f,
		// zero-valued ch, the same as it is in the `time` pkg
	}
	fc.addTimer(s)
	return s
}

func (fc *fakeClock) addTimer(s *sleeper) {
	fc.l.Lock()
	defer fc.l.Unlock()
	now := fc.time
	if now.Sub(s.until) >= 0 {
		// special case - trigger immediately
		s.awaken(now)
	} else {
		// otherwise, add to the set of sleepers
		fc.sleepers = append(fc.sleepers, s)
		// and notify any blockers
		fc.blockers = notifyBlockers(fc.blockers, len(fc.sleepers))
	}
}

func sendTime(c interface{}, now time.Time) {
	c.(chan time.Time) <- now
}

func goFunc(fn interface{}, _ time.Time) {
	go fn.(func())()
}

// notifyBlockers notifies all the blockers waiting until the
// given number of sleepers are waiting on the fakeClock. It
// returns an updated slice of blockers (i.e. those still waiting)
func notifyBlockers(blockers []*blocker, count int) (newBlockers []*blocker) {
	for _, b := range blockers {
		if b.count == count {
			close(b.ch)
		} else {
			newBlockers = append(newBlockers, b)
		}
	}
	return
}

// notifySleepers finds and notifies all the sleepers waiting until time t.
func notifySleepers(sleepers []*sleeper, t time.Time) []*sleeper {
	var newSleepers []*sleeper
	for _, s := range sleepers {
		if t.Sub(s.Until()) >= 0 {
			s.awaken(t)
		} else {
			newSleepers = append(newSleepers, s)
		}
	}
	return newSleepers
}

// Sleep blocks until the given duration has passed on the fakeClock
func (fc *fakeClock) Sleep(d time.Duration) {
	<-fc.After(d)
}

// Time returns the current time of the fakeClock
func (fc *fakeClock) Now() time.Time {
	fc.l.RLock()
	defer fc.l.RUnlock()
	return fc.time
}

// Since returns the duration that has passed since the given time on the fakeClock
func (fc *fakeClock) Since(t time.Time) time.Duration {
	return fc.Now().Sub(t)
}

func (fc *fakeClock) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic(errors.New("non-positive interval for NewTicker"))
	}
	ft := &fakeTicker{
		c:      make(chan time.Time, 1),
		stop:   make(chan bool, 1),
		clock:  fc,
		period: d,
	}
	ft.runTickThread()
	return ft
}

// set sets the fakeClock and notifies sleepers and blockers before returning.
// The caller must hold fc.l for the duration.
func (fc *fakeClock) set(t time.Time) {
	fc.sleepers = notifySleepers(fc.sleepers, t)
	fc.blockers = notifyBlockers(fc.blockers, len(fc.sleepers))
	fc.time = t
}

// Advance advances fakeClock to a new point in time, ensuring channels from any
// previous invocations of After are notified appropriately before returning
func (fc *fakeClock) Advance(d time.Duration) {
	fc.l.Lock()
	defer fc.l.Unlock()
	fc.set(fc.time.Add(d))
}

// Set sets the FakeClock to a new point in time, ensuring channels from any
// previous invocations of After are notified appropriately before returning
func (fc *fakeClock) Set(t time.Time) {
	fc.l.Lock()
	defer fc.l.Unlock()
	fc.set(t)
}

// BlockUntil will block until the fakeClock has the given number of sleepers
// (callers of Sleep or After)
func (fc *fakeClock) BlockUntil(n int) {
	fc.l.Lock()
	// Fast path: current number of sleepers is what we're looking for
	if len(fc.sleepers) == n {
		fc.l.Unlock()
		return
	}
	// Otherwise, set up a new blocker
	b := &blocker{
		count: n,
		ch:    make(chan struct{}),
	}
	fc.blockers = append(fc.blockers, b)
	fc.l.Unlock()
	<-b.ch
}
