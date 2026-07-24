//go:build !windows && !linux

package main

import "fmt"

type processMeter struct{}

func newProcessMeter(_ string, _ int) (*processMeter, error) {
	return nil, fmt.Errorf("process metrics are only supported on Windows and Linux")
}

func (m *processMeter) close() {}

func (m *processMeter) sample() (processSample, error) {
	return processSample{}, fmt.Errorf("process metrics are not supported on this platform")
}
