//go:build linux

package media

import (
	"fmt"

	"draarl/internal/config"

	"golang.org/x/sys/unix"
)

func applyMediaProcessLimits(pid, memoryLimitMB, cpuLimitSeconds int) error {
	if memoryLimitMB <= 0 {
		memoryLimitMB = config.DefaultBroadcastTranscodeMemoryMB
	}
	if cpuLimitSeconds <= 0 {
		cpuLimitSeconds = config.DefaultBroadcastTranscodeCPUSeconds
	}
	memoryBytes := uint64(memoryLimitMB) * 1024 * 1024
	if err := unix.Prlimit(pid, unix.RLIMIT_AS, &unix.Rlimit{Cur: memoryBytes, Max: memoryBytes}, nil); err != nil {
		return fmt.Errorf("apply ffmpeg address-space limit: %w", err)
	}
	cpuSeconds := uint64(cpuLimitSeconds)
	if err := unix.Prlimit(pid, unix.RLIMIT_CPU, &unix.Rlimit{Cur: cpuSeconds, Max: cpuSeconds + 1}, nil); err != nil {
		return fmt.Errorf("apply ffmpeg CPU limit: %w", err)
	}
	return nil
}
