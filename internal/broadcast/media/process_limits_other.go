//go:build !linux

package media

func applyMediaProcessLimits(pid, memoryLimitMB, cpuLimitSeconds int) error {
	return nil
}
