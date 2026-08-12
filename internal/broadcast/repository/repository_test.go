package repository

import (
	"errors"
	"testing"
	"time"

	"draarl/internal/broadcast/model"
)

func TestNormalizeScheduleInterval(t *testing.T) {
	schedule := &model.BroadcastSchedule{
		Name: " repeated notice ", ScheduleType: model.ScheduleTypeInterval,
		Timezone: "Asia/Shanghai", LocalTime: "08:00:00", WeekdayMask: 2,
		IntervalSeconds: 90 * 60,
		IntervalStartAt: timePointerForTest("2026-08-09T00:00:00Z"),
	}
	if err := normalizeSchedule(schedule); err != nil {
		t.Fatalf("normalizeSchedule() error = %v", err)
	}
	if schedule.Name != "repeated notice" || schedule.LocalTime != "" || schedule.WeekdayMask != 0 || schedule.IntervalSeconds != 90*60 || schedule.IntervalStartAt == nil {
		t.Fatalf("unexpected normalized interval schedule: %#v", schedule)
	}
}

func TestNormalizeScheduleRejectsInvalidInterval(t *testing.T) {
	for _, seconds := range []int{model.MinScheduleIntervalSeconds - 1, model.MinScheduleIntervalSeconds + 1} {
		schedule := &model.BroadcastSchedule{
			Name: "invalid interval", ScheduleType: model.ScheduleTypeInterval,
			Timezone: "Asia/Shanghai", IntervalSeconds: seconds, IntervalStartAt: timePointerForTest("2026-08-09T00:00:00Z"),
		}
		if err := normalizeSchedule(schedule); !errors.Is(err, ErrInvalidSchedule) {
			t.Fatalf("normalizeSchedule(%d) error = %v, want ErrInvalidSchedule", seconds, err)
		}
	}
}

func TestNormalizeScheduleRequiresIntervalZeroPoint(t *testing.T) {
	schedule := &model.BroadcastSchedule{
		Name: "missing zero point", ScheduleType: model.ScheduleTypeInterval,
		Timezone: "Asia/Shanghai", IntervalSeconds: 15 * 60,
	}
	if err := normalizeSchedule(schedule); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("normalizeSchedule() error = %v, want ErrInvalidSchedule", err)
	}
}

func TestNormalizeScheduleBlackoutWindow(t *testing.T) {
	schedule := &model.BroadcastSchedule{
		Name: "quiet night", ScheduleType: model.ScheduleTypeInterval,
		Timezone: "Asia/Shanghai", IntervalSeconds: 15 * 60,
		IntervalStartAt:   timePointerForTest("2026-08-09T00:00:00Z"),
		BlackoutStartTime: " 22:00 ", BlackoutEndTime: "07:30",
	}
	if err := normalizeSchedule(schedule); err != nil {
		t.Fatalf("normalizeSchedule() error = %v", err)
	}
	if schedule.BlackoutStartTime != "22:00:00" || schedule.BlackoutEndTime != "07:30:00" {
		t.Fatalf("unexpected blackout window: %q-%q", schedule.BlackoutStartTime, schedule.BlackoutEndTime)
	}
}

func TestNormalizeScheduleRejectsInvalidBlackoutWindow(t *testing.T) {
	for _, window := range [][2]string{{"22:00", ""}, {"", "07:00"}, {"24:00", "07:00"}, {"07:00", "07:00:00"}} {
		schedule := &model.BroadcastSchedule{
			Name: "invalid blackout", ScheduleType: model.ScheduleTypeDaily,
			Timezone: "Asia/Shanghai", LocalTime: "12:00",
			BlackoutStartTime: window[0], BlackoutEndTime: window[1],
		}
		if err := normalizeSchedule(schedule); !errors.Is(err, ErrInvalidSchedule) {
			t.Fatalf("normalizeSchedule(%q-%q) error = %v, want ErrInvalidSchedule", window[0], window[1], err)
		}
	}
}

func timePointerForTest(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return &parsed
}
