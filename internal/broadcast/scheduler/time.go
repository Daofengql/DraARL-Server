package scheduler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"draarl/internal/broadcast/model"
)

// NextOccurrence returns the first theoretical occurrence strictly after
// after. Repeated wall-clock times execute only their first occurrence, while
// nonexistent DST times are skipped.
func NextOccurrence(schedule *model.BroadcastSchedule, after time.Time) (*time.Time, error) {
	if schedule == nil {
		return nil, fmt.Errorf("schedule is nil")
	}
	if !model.IsScheduleType(schedule.ScheduleType) {
		return nil, fmt.Errorf("unsupported schedule type %q", schedule.ScheduleType)
	}
	if schedule.ScheduleType == model.ScheduleTypeInterval {
		return nextIntervalOccurrence(schedule, after)
	}
	if schedule.ScheduleType == model.ScheduleTypeOnce {
		if schedule.ScheduledAt == nil {
			return nil, fmt.Errorf("scheduled_at is required for once schedule")
		}
		candidate := schedule.ScheduledAt.UTC()
		if !candidate.After(after.UTC()) {
			return nil, nil
		}
		return &candidate, nil
	}

	location, err := time.LoadLocation(strings.TrimSpace(schedule.Timezone))
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}
	hour, minute, second, err := parseLocalTime(schedule.LocalTime)
	if err != nil {
		return nil, err
	}
	if schedule.ScheduleType == model.ScheduleTypeWeekly && schedule.WeekdayMask == 0 {
		return nil, fmt.Errorf("weekday_mask is required for weekly schedule")
	}

	localAfter := after.In(location)
	startDate := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), 12, 0, 0, 0, location)
	for dayOffset := 0; dayOffset < 370; dayOffset++ {
		day := startDate.AddDate(0, 0, dayOffset)
		if schedule.ScheduleType == model.ScheduleTypeWeekly && schedule.WeekdayMask&(1<<uint(day.Weekday())) == 0 {
			continue
		}
		instants := wallClockInstants(location, day.Year(), day.Month(), day.Day(), hour, minute, second)
		if len(instants) == 0 {
			continue
		}
		// The first instant is the only valid occurrence on a fall-back day.
		candidate := instants[0]
		if candidate.After(after) {
			candidate = candidate.UTC()
			return &candidate, nil
		}
	}
	return nil, fmt.Errorf("no valid occurrence found within 370 days")
}

func nextIntervalOccurrence(schedule *model.BroadcastSchedule, after time.Time) (*time.Time, error) {
	if schedule.IntervalSeconds < model.MinScheduleIntervalSeconds || schedule.IntervalSeconds > model.MaxScheduleIntervalSeconds || schedule.IntervalSeconds%60 != 0 {
		return nil, fmt.Errorf("interval_seconds must be a whole minute between %d and %d", model.MinScheduleIntervalSeconds, model.MaxScheduleIntervalSeconds)
	}
	if schedule.IntervalStartAt == nil {
		return nil, fmt.Errorf("interval_start_at is required")
	}
	interval := time.Duration(schedule.IntervalSeconds) * time.Second
	after = after.UTC()
	start := schedule.IntervalStartAt.UTC()
	if start.After(after) {
		return &start, nil
	}
	steps := after.Sub(start)/interval + 1
	candidate := start.Add(steps * interval)
	return &candidate, nil
}

func parseLocalTime(value string) (int, int, int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("local_time must use HH:MM or HH:MM:SS")
	}
	values := make([]int, 3)
	for i, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid local_time")
		}
		values[i] = parsed
	}
	if values[0] < 0 || values[0] > 23 || values[1] < 0 || values[1] > 59 || values[2] < 0 || values[2] > 59 {
		return 0, 0, 0, fmt.Errorf("invalid local_time")
	}
	return values[0], values[1], values[2], nil
}

func wallClockInstants(location *time.Location, year int, month time.Month, day, hour, minute, second int) []time.Time {
	naiveUTC := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
	offsets := make(map[int]struct{})
	for delta := -2; delta <= 2; delta++ {
		probe := time.Date(year, month, day, 12, 0, 0, 0, location).AddDate(0, 0, delta)
		_, offset := probe.Zone()
		offsets[offset] = struct{}{}
	}

	result := make([]time.Time, 0, 2)
	for offset := range offsets {
		candidate := naiveUTC.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(location)
		if local.Year() == year && local.Month() == month && local.Day() == day &&
			local.Hour() == hour && local.Minute() == minute && local.Second() == second {
			result = append(result, candidate)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Before(result[j]) })
	return result
}
