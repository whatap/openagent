package counter

import (
	"io/ioutil"
	"math"
	"runtime"
	"strconv"
	"strings"
)

// GetCPUCores returns the number of CPU cores.
// In container environments with cgroup CPU limits, returns the limited value.
func GetCPUCores() int32 {
	if cgroupCores := getCgroupCpuLimitFloat(); cgroupCores > 0 {
		return int32(math.Ceil(cgroupCores))
	}
	return int32(runtime.NumCPU())
}

// getCgroupCpuLimitFloat returns CPU core count from cgroup CPU limits as float64.
// Returns 0 if no limit is set or detection fails.
func getCgroupCpuLimitFloat() float64 {
	if runtime.GOOS != "linux" {
		return 0
	}

	// cgroup v2: /sys/fs/cgroup/cpu.max
	// format: "$MAX $PERIOD" (e.g., "200000 100000" = 2 cores)
	// "max 100000" means unlimited
	if data, err := ioutil.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		fields := strings.Fields(strings.TrimSpace(string(data)))
		if len(fields) == 2 && fields[0] != "max" {
			quota, err1 := strconv.ParseFloat(fields[0], 64)
			period, err2 := strconv.ParseFloat(fields[1], 64)
			if err1 == nil && err2 == nil && period > 0 {
				return quota / period
			}
		}
		return 0
	}

	// cgroup v1: /sys/fs/cgroup/cpu/cpu.cfs_quota_us + cpu.cfs_period_us
	// quota -1 means unlimited
	quotaData, err1 := ioutil.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	periodData, err2 := ioutil.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if err1 != nil || err2 != nil {
		return 0
	}
	quota, err1 := strconv.ParseFloat(strings.TrimSpace(string(quotaData)), 64)
	period, err2 := strconv.ParseFloat(strings.TrimSpace(string(periodData)), 64)
	if err1 != nil || err2 != nil || period <= 0 || quota <= 0 {
		return 0
	}
	return quota / period
}
