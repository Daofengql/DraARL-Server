//go:build !windows

package handler

import "syscall"

func readSystemDisk() systemDisk {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil || stat.Blocks == 0 {
		return systemDisk{}
	}
	if stat.Bsize <= 0 {
		return systemDisk{}
	}
	// Statfs reports a positive block size on every supported Unix ABI. Keep
	// the checked conversion local so the signed platform field cannot wrap.
	blockSize := uint64(stat.Bsize) // #nosec G115 -- checked positive Statfs block size.
	total := stat.Blocks * blockSize
	free := stat.Bfree * blockSize
	available := stat.Bavail * blockSize
	if available > total {
		available = total
	}
	used := total - available
	return systemDisk{Available: true, TotalBytes: total, UsedBytes: used, FreeBytes: free, UsedPercent: float64(used) * 100 / float64(total)}
}
