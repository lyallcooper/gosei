package docker

import (
	"encoding/json"
	"io"

	"github.com/docker/docker/api/types/container"
)

func decodeStats(r io.Reader, stats *container.StatsResponse) error {
	return json.NewDecoder(r).Decode(stats)
}

func newStatsDecoder(r io.Reader) *json.Decoder {
	return json.NewDecoder(r)
}

func calculateStats(id string, stats *container.StatsResponse) *ContainerStats {
	result := &ContainerStats{
		ID:          id,
		MemoryUsage: stats.MemoryStats.Usage,
		MemoryLimit: stats.MemoryStats.Limit,
	}

	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)

	if systemDelta > 0 && cpuDelta > 0 {
		cpuCount := float64(stats.CPUStats.OnlineCPUs)
		if cpuCount == 0 {
			cpuCount = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
		}
		if cpuCount == 0 {
			cpuCount = 1
		}
		result.CPUPercent = (cpuDelta / systemDelta) * cpuCount * 100.0
	}

	if result.MemoryLimit > 0 {
		memUsage := stats.MemoryStats.Usage
		if cache, ok := stats.MemoryStats.Stats["cache"]; ok {
			memUsage -= cache
		}
		result.MemoryUsage = memUsage
		result.MemoryPercent = float64(memUsage) / float64(result.MemoryLimit) * 100.0
	}

	for _, network := range stats.Networks {
		result.NetworkRx += network.RxBytes
		result.NetworkTx += network.TxBytes
	}

	return result
}
