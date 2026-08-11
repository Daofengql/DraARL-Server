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

func timePointerForTest(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return &parsed
}
