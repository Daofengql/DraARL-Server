//go:build !windows

package handler

import "syscall"

func readSystemDisk() systemDisk {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil || stat.Blocks == 0 {
		return systemDisk{}
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	available := stat.Bavail * uint64(stat.Bsize)
	if available > total {
		available = total
	}
	used := total - available
	return systemDisk{Available: true, TotalBytes: total, UsedBytes: used, FreeBytes: free, UsedPercent: float64(used) * 100 / float64(total)}
}
