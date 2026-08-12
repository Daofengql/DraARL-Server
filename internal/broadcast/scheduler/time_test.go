package scheduler

import (
	"testing"
	"time"

	"draarl/internal/broadcast/model"
)

func TestNextOccurrence(t *testing.T) {
	tests := []struct {
		name     string
		schedule model.BroadcastSchedule
		after    string
		want     string
	}{
		{
			name:     "once future",
			schedule: model.BroadcastSchedule{ScheduleType: model.ScheduleTypeOnce, ScheduledAt: timePointer(mustTime(t, "2026-08-09T04:00:00Z"))},
			after:    "2026-08-09T03:59:59Z", want: "2026-08-09T04:00:00Z",
		},
		{
			name:     "daily shanghai",
			schedule: model.BroadcastSchedule{ScheduleType: model.ScheduleTypeDaily, Timezone: "Asia/Shanghai", LocalTime: "12:00:00"},
			after:    "2026-08-09T03:59:59Z", want: "2026-08-09T04:00:00Z",
		},
		{
			name:     "weekly monday",
			schedule: model.BroadcastSchedule{ScheduleType: model.ScheduleTypeWeekly, Timezone: "Asia/Shanghai", LocalTime: "08:30:00", WeekdayMask: 1 << uint(time.Monday)},
			after:    "2026-08-09T00:00:00Z", want: "2026-08-10T00:30:00Z",
		},
		{
			name:     "interval starts after requested duration",
			schedule: model.BroadcastSchedule{ScheduleType: model.ScheduleTypeInterval, IntervalSeconds: 15 * 60, IntervalStartAt: timePointer(mustTime(t, "2026-08-09T04:00:00Z"))},
			after:    "2026-08-09T03:30:00Z", want: "2026-08-09T04:00:00Z",
		},
		{
			name: "interval advances from zero point",
			schedule: model.BroadcastSchedule{
				ScheduleType: model.ScheduleTypeInterval, IntervalSeconds: 15 * 60,
				IntervalStartAt: timePointer(mustTime(t, "2026-08-09T04:00:00Z")),
			},
			after: "2026-08-09T04:10:00Z", want: "2026-08-09T04:15:00Z",
		},
		{
			name: "interval skips missed occurrences without drifting",
			schedule: model.BroadcastSchedule{
				ScheduleType: model.ScheduleTypeInterval, IntervalSeconds: 15 * 60,
				IntervalStartAt: timePointer(mustTime(t, "2026-08-09T04:00:00Z")),
			},
			after: "2026-08-09T04:31:00Z", want: "2026-08-09T04:45:00Z",
		},
		{
			name: "interval skips daytime blackout",
			schedule: model.BroadcastSchedule{
				ScheduleType: model.ScheduleTypeInterval, Timezone: "Asia/Shanghai",
				IntervalSeconds: 15 * 60, IntervalStartAt: timePointer(mustTime(t, "2026-08-09T00:00:00Z")),
				BlackoutStartTime: "08:30:00", BlackoutEndTime: "10:00:00",
			},
			after: "2026-08-09T00:15:00Z", want: "2026-08-09T02:00:00Z",
		},
		{
			name: "interval skips overnight blackout",
			schedule: model.BroadcastSchedule{
				ScheduleType: model.ScheduleTypeInterval, Timezone: "Asia/Shanghai",
				IntervalSeconds: 30 * 60, IntervalStartAt: timePointer(mustTime(t, "2026-08-09T13:00:00Z")),
				BlackoutStartTime: "22:00", BlackoutEndTime: "07:00",
			},
			after: "2026-08-09T13:30:00Z", want: "2026-08-09T23:00:00Z",
		},
		{
			name: "blackout end boundary is allowed",
			schedule: model.BroadcastSchedule{
				ScheduleType: model.ScheduleTypeInterval, Timezone: "Asia/Shanghai",
				IntervalSeconds: 30 * 60, IntervalStartAt: timePointer(mustTime(t, "2026-08-09T13:00:00Z")),
				BlackoutStartTime: "22:00:00", BlackoutEndTime: "23:00:00",
			},
			after: "2026-08-09T13:30:00Z", want: "2026-08-09T15:00:00Z",
		},
		{
			name:     "skip nonexistent DST time",
			schedule: model.BroadcastSchedule{ScheduleType: model.ScheduleTypeDaily, Timezone: "America/New_York", LocalTime: "02:30:00"},
			after:    "2026-03-08T05:00:00Z", want: "2026-03-09T06:30:00Z",
		},
		{
			name:     "fall back chooses first instant",
			schedule: model.BroadcastSchedule{ScheduleType: model.ScheduleTypeDaily, Timezone: "America/New_York", LocalTime: "01:30:00"},
			after:    "2026-11-01T04:00:00Z", want: "2026-11-01T05:30:00Z",
		},
		{
			name:     "fall back never chooses second instant",
			schedule: model.BroadcastSchedule{ScheduleType: model.ScheduleTypeDaily, Timezone: "America/New_York", LocalTime: "01:30:00"},
			after:    "2026-11-01T05:45:00Z", want: "2026-11-02T06:30:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextOccurrence(&tt.schedule, mustTime(t, tt.after))
			if err != nil {
				t.Fatalf("NextOccurrence() error = %v", err)
			}
			if got == nil || got.Format(time.RFC3339) != tt.want {
				t.Fatalf("NextOccurrence() = %v, want %s", got, tt.want)
			}
		})
	}
}

func TestNextOccurrenceOnceExpired(t *testing.T) {
	at := mustTime(t, "2026-08-09T04:00:00Z")
	got, err := NextOccurrence(&model.BroadcastSchedule{ScheduleType: model.ScheduleTypeOnce, ScheduledAt: &at}, at)
	if err != nil || got != nil {
		t.Fatalf("expired once = %v, %v; want nil, nil", got, err)
	}
}

func TestNextOccurrenceRejectsInvalidInterval(t *testing.T) {
	for _, seconds := range []int{59, 61} {
		start := time.Now()
		_, err := NextOccurrence(&model.BroadcastSchedule{ScheduleType: model.ScheduleTypeInterval, IntervalSeconds: seconds, IntervalStartAt: &start}, start)
		if err == nil {
			t.Fatalf("interval %d seconds should be rejected", seconds)
		}
	}
}

func TestNextOccurrenceRejectsIntervalWithoutZeroPoint(t *testing.T) {
	_, err := NextOccurrence(&model.BroadcastSchedule{ScheduleType: model.ScheduleTypeInterval, IntervalSeconds: 15 * 60}, time.Now())
	if err == nil {
		t.Fatal("interval without a zero point should be rejected")
	}
}

func TestNextOccurrenceRejectsInvalidBlackoutWindow(t *testing.T) {
	start := mustTime(t, "2026-08-09T00:00:00Z")
	tests := []model.BroadcastSchedule{
		{ScheduleType: model.ScheduleTypeInterval, Timezone: "UTC", IntervalSeconds: 60, IntervalStartAt: &start, BlackoutStartTime: "22:00"},
		{ScheduleType: model.ScheduleTypeInterval, Timezone: "UTC", IntervalSeconds: 60, IntervalStartAt: &start, BlackoutStartTime: "25:00", BlackoutEndTime: "07:00"},
		{ScheduleType: model.ScheduleTypeInterval, Timezone: "UTC", IntervalSeconds: 60, IntervalStartAt: &start, BlackoutStartTime: "07:00", BlackoutEndTime: "07:00"},
	}
	for _, schedule := range tests {
		if _, err := NextOccurrence(&schedule, start); err == nil {
			t.Fatalf("invalid blackout window should be rejected: %#v", schedule)
		}
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func timePointer(value time.Time) *time.Time { return &value }
