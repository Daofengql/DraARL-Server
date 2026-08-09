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
