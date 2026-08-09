//go:build windows

package main

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	psapiDLL   = windows.NewLazySystemDLL("psapi.dll")
	getProcMem = psapiDLL.NewProc("GetProcessMemoryInfo")
)

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type processMeter struct {
	name   string
	handle windows.Handle
}

func newProcessMeter(name string, pid int) (*processMeter, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return nil, err
	}
	return &processMeter{name: name, handle: handle}, nil
}

func (m *processMeter) close() {
	if m != nil && m.handle != 0 {
		_ = windows.CloseHandle(m.handle)
		m.handle = 0
	}
}

func (m *processMeter) sample() (processSample, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(m.handle, &creation, &exit, &kernel, &user); err != nil {
		return processSample{}, err
	}
	mem := processMemoryCounters{CB: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	result, _, callErr := getProcMem.Call(uintptr(m.handle), uintptr(unsafe.Pointer(&mem)), uintptr(mem.CB))
	if result == 0 {
		return processSample{}, fmt.Errorf("GetProcessMemoryInfo(%s): %v", m.name, callErr)
	}
	return processSample{
		cpu: time.Duration(kernel.Nanoseconds() + user.Nanoseconds()),
		rss: uint64(mem.WorkingSetSize),
	}, nil
}
