package system

import (
	"errors"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

type Stats struct {
	CPUPercent float64
	MemUsed    uint64
	MemTotal   uint64
	Uptime     uint64
}

// Collect samples host CPU/memory/uptime. CPU usage is measured against the
// previous call (non-blocking), so the first call after process start always
// reports 0. Partial failures don't clear stats already collected; errors
// are joined and returned for the caller to log.
func Collect() (*Stats, error) {
	stats := &Stats{}
	var errs []error

	if percents, err := cpu.Percent(0, false); err != nil {
		errs = append(errs, err)
	} else if len(percents) > 0 {
		stats.CPUPercent = percents[0]
	}

	if vm, err := mem.VirtualMemory(); err != nil {
		errs = append(errs, err)
	} else {
		stats.MemUsed = vm.Used
		stats.MemTotal = vm.Total
	}

	if uptime, err := host.Uptime(); err != nil {
		errs = append(errs, err)
	} else {
		stats.Uptime = uptime
	}

	return stats, errors.Join(errs...)
}
