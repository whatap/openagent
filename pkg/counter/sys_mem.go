package counter

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// memResult holds memory usage percentages
type memResult struct {
	Mem  float32 // system memory used %
	Swap float32 // swap used %
}

// collectMemPercent reads /proc/meminfo and calculates memory usage percentages
func collectMemPercent() *memResult {
	if runtime.GOOS != "linux" {
		return &memResult{}
	}

	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return &memResult{}
	}
	defer f.Close()

	var memTotal, memAvailable, memFree, buffers, cached uint64
	var swapTotal, swapFree uint64
	hasAvailable := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		val, _ := strconv.ParseUint(fields[1], 10, 64)
		// values in /proc/meminfo are in kB
		val *= 1024

		switch fields[0] {
		case "MemTotal:":
			memTotal = val
		case "MemFree:":
			memFree = val
		case "MemAvailable:":
			memAvailable = val
			hasAvailable = true
		case "Buffers:":
			buffers = val
		case "Cached:":
			cached = val
		case "SwapTotal:":
			swapTotal = val
		case "SwapFree:":
			swapFree = val
		}
	}

	result := &memResult{}
	if memTotal > 0 {
		var used uint64
		if hasAvailable {
			used = memTotal - memAvailable
		} else {
			used = memTotal - memFree - buffers - cached
		}
		result.Mem = float32(used*100) / float32(memTotal)
	}
	if swapTotal > 0 {
		swapUsed := swapTotal - swapFree
		result.Swap = float32(swapUsed*100) / float32(swapTotal)
	}
	return result
}
