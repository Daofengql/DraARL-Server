//go:build linux

package media

import (
	"fmt"
	"os/exec"
	"strconv"

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

// prepareMediaCommand applies limits before the target executable is started.
// A post-Start prlimit call leaves a window in which an untrusted media file
// can consume resources without the configured limits. The shell sets the
// inherited rlimits and immediately execs the original command, so ffmpeg and
// ffprobe start with the limits already installed.
func prepareMediaCommand(command *exec.Cmd, memoryLimitMB, cpuLimitSeconds int) error {
	if command == nil || command.Path == "" {
		return fmt.Errorf("media command is empty")
	}
	if memoryLimitMB <= 0 {
		memoryLimitMB = config.DefaultBroadcastTranscodeMemoryMB
	}
	if cpuLimitSeconds <= 0 {
		cpuLimitSeconds = config.DefaultBroadcastTranscodeCPUSeconds
	}
	args := command.Args
	if len(args) == 0 {
		args = []string{command.Path}
	}
	forward := append([]string{command.Path}, args[1:]...)
	command.Path = "/bin/sh"
	command.Args = append([]string{
		"sh", "-c",
		`ulimit -v "$1" && ulimit -t "$2" && shift 2 && exec "$@"`,
		"draarl-media-limits",
		strconv.FormatInt(int64(memoryLimitMB)*1024, 10),
		strconv.Itoa(cpuLimitSeconds),
	}, forward...)
	return nil
}
