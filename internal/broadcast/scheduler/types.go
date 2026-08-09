package scheduler

import "draarl/internal/broadcast/model"

type RunExecution struct {
	Run      model.BroadcastRun
	Schedule model.BroadcastSchedule
	Audio    model.BroadcastAudio
}
