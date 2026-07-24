//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type processMeter struct {
	name    string
	procDir string
}

func newProcessMeter(name string, pid int) (*processMeter, error) {
	meter := &processMeter{name: name, procDir: filepath.Join("/proc", strconv.Itoa(pid))}
	if _, err := meter.sample(); err != nil {
		return nil, err
	}
	return meter, nil
}

func (m *processMeter) close() {}

func (m *processMeter) sample() (processSample, error) {
	taskDir := filepath.Join(m.procDir, "task")
	tasks, err := os.ReadDir(taskDir)
	if err != nil {
		return processSample{}, fmt.Errorf("read %s process tasks: %w", m.name, err)
	}
	var cpuNanos int64
	for _, task := range tasks {
		schedData, readErr := os.ReadFile(filepath.Join(taskDir, task.Name(), "schedstat"))
		if readErr != nil {
			// A thread can exit between ReadDir and ReadFile. The remaining live
			// threads still provide a useful process sample.
			if os.IsNotExist(readErr) {
				continue
			}
			return processSample{}, fmt.Errorf("read %s thread %s schedstat: %w", m.name, task.Name(), readErr)
		}
		schedFields := strings.Fields(string(schedData))
		if len(schedFields) < 1 {
			return processSample{}, fmt.Errorf("invalid %s thread %s schedstat", m.name, task.Name())
		}
		threadNanos, parseErr := strconv.ParseInt(schedFields[0], 10, 64)
		if parseErr != nil {
			return processSample{}, fmt.Errorf("parse %s thread %s CPU time: %w", m.name, task.Name(), parseErr)
		}
		cpuNanos += threadNanos
	}

	statusData, err := os.ReadFile(filepath.Join(m.procDir, "status"))
	if err != nil {
		return processSample{}, fmt.Errorf("read %s process status: %w", m.name, err)
	}
	var rssBytes uint64
	for _, line := range strings.Split(string(statusData), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			rssKB, parseErr := strconv.ParseUint(fields[1], 10, 64)
			if parseErr != nil {
				return processSample{}, fmt.Errorf("parse %s process RSS: %w", m.name, parseErr)
			}
			rssBytes = rssKB * 1024
		}
		break
	}
	return processSample{cpu: time.Duration(cpuNanos), rss: rssBytes}, nil
}
