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
	schedData, err := os.ReadFile(filepath.Join(m.procDir, "schedstat"))
	if err != nil {
		return processSample{}, fmt.Errorf("read %s process schedstat: %w", m.name, err)
	}
	schedFields := strings.Fields(string(schedData))
	if len(schedFields) < 1 {
		return processSample{}, fmt.Errorf("invalid %s process schedstat", m.name)
	}
	cpuNanos, err := strconv.ParseInt(schedFields[0], 10, 64)
	if err != nil {
		return processSample{}, fmt.Errorf("parse %s process CPU time: %w", m.name, err)
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
