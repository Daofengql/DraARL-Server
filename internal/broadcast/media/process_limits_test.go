//go:build linux

package media

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"draarl/internal/config"
)

func TestMediaProcessLimitsHaveSafeDefaults(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "sleep 0.1")
	processor := &Processor{config: config.BroadcastConfig{}}
	if err := processor.runCommand(command); err != nil {
		t.Fatalf("run command with default process limits: %v", err)
	}
}

func TestRunCommandInstallsLimitsBeforeExec(t *testing.T) {
	path := t.TempDir() + "/limits.txt"
	command := exec.Command("/bin/sh", "-c", `awk '/Max address space|Max cpu time/ {print}' /proc/self/limits > "$1"`, "probe", path)
	processor := &Processor{config: config.BroadcastConfig{TranscodeMemoryLimitMB: 768, TranscodeCPULimitSeconds: 17}}
	if err := processor.runCommand(command); err != nil {
		t.Fatal(err)
	}
	limits, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(limits)
	if !linuxLimitValue(text, "Max address space", "805306368") || !linuxLimitValue(text, "Max cpu time", "17") {
		t.Fatalf("exec-time rlimits missing expected values: %s", text)
	}
}

func TestApplyMediaProcessLimitsUpdatesChildRlimits(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "sleep 5")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	if err := applyMediaProcessLimits(command.Process.Pid, 768, 17); err != nil {
		t.Fatal(err)
	}
	limits, err := os.ReadFile(fmt.Sprintf("/proc/%d/limits", command.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	text := string(limits)
	if !linuxLimitValue(text, "Max address space", "805306368") || !linuxLimitValue(text, "Max cpu time", "17") {
		t.Fatalf("child rlimits missing expected values: %s", text)
	}
}

func linuxLimitValue(limits, name, value string) bool {
	for _, line := range strings.Split(limits, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.Join(fields[:3], " ") == name && fields[3] == value {
			return true
		}
	}
	return false
}
