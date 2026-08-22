//go:build !linux

package media

import (
	"fmt"
	"log"
	"os/exec"
	"sync"
)

var nonLinuxLimitWarned sync.Once

func applyMediaProcessLimits(pid, memoryLimitMB, cpuLimitSeconds int) error {
	// 非 Linux 平台无 RLIMIT 机制，无法施加 ffmpeg 内存/CPU 限制；仅提示一次，
	// 让运维知晓该平台上的转码资源限制未生效。
	nonLinuxLimitWarned.Do(func() {
		log.Printf("[BROADCAST] 当前平台不支持 ffmpeg 进程内存/CPU 限制（仅 Linux 生效），请通过系统级 cgroup/资源编排限制")
	})
	return nil
}

func prepareMediaCommand(command *exec.Cmd, memoryLimitMB, cpuLimitSeconds int) error {
	if command == nil || command.Path == "" {
		return fmt.Errorf("media command is empty")
	}
	return nil
}
