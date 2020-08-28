package clockwork

import (
	"reflect"
	"testing"
	"time"
)

func TestFakeClockAfter(t *testing.T) {
	fc := &fakeClock{}

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
	t1 := time.Date(1999, time.February, 3, 4, 5, 6, 7, time.UTC)
	fc := NewFakeClockAt(t1)
	now := fc.Now()
	if !reflect.DeepEqual(now, t1) {
		t.Fatalf("fakeClock.Now() returned unexpected non-initialised value: want=%#v, got %#v", t1, now)
	}
}

func TestFakeClockSince(t *testing.T) {
	fc := NewFakeClock()
	now := fc.Now()
	elapsedTime := time.Second
	fc.Advance(elapsedTime)
	if fc.Since(now) != elapsedTime {
		t.Fatalf("fakeClock.Since() returned unexpected duration, got: %d, want: %d", fc.Since(now), elapsedTime)
	}
}

func TestSet(t *testing.T) {
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
