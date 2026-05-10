package core

import (
	"testing"
	"time"
)

func TestNextPullTimeAt(t *testing.T) {
	loc := time.FixedZone("Test/Local", -5*60*60)

	tests := []struct {
		name  string
		sched PullSchedule
		now   time.Time
		want  time.Time
	}{
		{
			name:  "daily time already passed today",
			sched: PullSchedule{Mode: "daily", Time: "14:00"},
			now:   time.Date(2026, time.May, 4, 14, 30, 0, 0, loc),
			want:  time.Date(2026, time.May, 5, 14, 0, 0, 0, loc),
		},
		{
			name:  "daily time not yet reached",
			sched: PullSchedule{Mode: "daily", Time: "14:00"},
			now:   time.Date(2026, time.May, 4, 10, 0, 0, 0, loc),
			want:  time.Date(2026, time.May, 4, 14, 0, 0, 0, loc),
		},
		{
			name:  "weekly correct day not yet",
			sched: PullSchedule{Mode: "weekly", Time: "09:00", DayOfWeek: int(time.Wednesday)},
			now:   time.Date(2026, time.May, 4, 10, 0, 0, 0, loc),
			want:  time.Date(2026, time.May, 6, 9, 0, 0, 0, loc),
		},
		{
			name:  "weekly correct day passed",
			sched: PullSchedule{Mode: "weekly", Time: "09:00", DayOfWeek: int(time.Monday)},
			now:   time.Date(2026, time.May, 4, 10, 0, 0, 0, loc),
			want:  time.Date(2026, time.May, 11, 9, 0, 0, 0, loc),
		},
		{
			name:  "weekly Sunday",
			sched: PullSchedule{Mode: "weekly", Time: "03:00", DayOfWeek: int(time.Sunday)},
			now:   time.Date(2026, time.May, 9, 10, 0, 0, 0, loc),
			want:  time.Date(2026, time.May, 10, 3, 0, 0, 0, loc),
		},
		{
			name:  "monthly day not yet reached",
			sched: PullSchedule{Mode: "monthly", Time: "02:00", DayOfMonth: 15},
			now:   time.Date(2026, time.May, 10, 10, 0, 0, 0, loc),
			want:  time.Date(2026, time.May, 15, 2, 0, 0, 0, loc),
		},
		{
			name:  "monthly day already passed",
			sched: PullSchedule{Mode: "monthly", Time: "02:00", DayOfMonth: 5},
			now:   time.Date(2026, time.May, 10, 10, 0, 0, 0, loc),
			want:  time.Date(2026, time.June, 5, 2, 0, 0, 0, loc),
		},
		{
			name:  "monthly February wraps",
			sched: PullSchedule{Mode: "monthly", Time: "12:00", DayOfMonth: 28},
			now:   time.Date(2026, time.February, 28, 13, 0, 0, 0, loc),
			want:  time.Date(2026, time.March, 28, 12, 0, 0, 0, loc),
		},
		{
			name:  "exact equality is strictly after",
			sched: PullSchedule{Mode: "daily", Time: "14:00"},
			now:   time.Date(2026, time.May, 4, 14, 0, 0, 0, loc),
			want:  time.Date(2026, time.May, 5, 14, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextPullTimeAt(tt.sched, tt.now); !got.Equal(tt.want) {
				t.Fatalf("nextPullTimeAt() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNextPullTimeAtInvalidSchedules(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 0, 0, 0, time.UTC)
	tests := []PullSchedule{
		{Mode: "daily", Time: "25:99"},
		{Mode: "", Time: "03:00"},
		{Mode: "weekly", Time: "03:00", DayOfWeek: 7},
		{Mode: "monthly", Time: "03:00", DayOfMonth: 29},
	}
	for _, sched := range tests {
		if got := nextPullTimeAt(sched, now); !got.IsZero() {
			t.Fatalf("nextPullTimeAt(%+v) = %s, want zero time", sched, got)
		}
	}
}

func TestParsePullIntervalSpecificDisablesTicker(t *testing.T) {
	if got := ParsePullInterval("specific"); got != 0 {
		t.Fatalf("ParsePullInterval(\"specific\") = %s, want 0", got)
	}
}
