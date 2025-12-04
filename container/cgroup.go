package container

import (
	"fmt"
	"os"
	"strconv"
)

func setupCgroup(cfg Config) string {
	cgroupBase := "/sys/fs/cgroup"
	cgroupName := fmt.Sprintf("jcontainer-%d", os.Getpid())
	cgroupPath := cgroupBase + "/" + cgroupName

	os.RemoveAll(cgroupPath)

	// Create cgroup directory
	must(os.MkdirAll(cgroupPath, 0755))
	fmt.Println("Created cgroup:", cgroupPath)

	// Set PID limit to 10 processes
	must(os.WriteFile(cgroupPath+"/pids.max", []byte(strconv.Itoa(cfg.Pids)), 0644))

	// Add memory limit
	memBytes := parseMemoryLimit(cfg.Memory)
	must(os.WriteFile(cgroupPath+"/memory.max", []byte(strconv.Itoa(memBytes)), 0644))

	// Add CPU limit
	quota, period := parseCPULimit(cfg.CPU)
	cpuMax := fmt.Sprintf("%d %d", quota, period)
	must(os.WriteFile(cgroupPath+"/cpu.max", []byte(cpuMax), 0644))

	return cgroupPath
}

func parseMemoryLimit(s string) int {
	if len(s) == 0 {
		return 104857600
	}
	last := s[len(s)-1]
	num := s[:len(s)-1]

	switch last {
	case 'M', 'm':
		v, _ := strconv.Atoi(num)
		return v * 1024 * 1024
	case 'G', 'g':
		v, _ := strconv.Atoi(num)
		return v * 1024 * 1024 * 1024
	default:
		v, _ := strconv.Atoi(s)
		return v
	}
}

func parseCPULimit(s string) (quota, period int) {
	if len(s) == 0 {
		return 50000, 100000
	}

	if s[len(s)-1] == '%' {
		v, _ := strconv.Atoi(s[:len(s)-1])
		period = 100000
		quota = v * period / 100
		return quota, period
	}

	var q, p int
	fmt.Sscanf(s, "%d:%d", &q, &p)
	if q == 0 || p == 0 {
		return 50000, 100000
	}
	return q, p
}

func cleanupCgroup(cgroupPath string) {
	// Remove the cgroup directory
	os.Remove(cgroupPath)
	fmt.Println("Cleaned up cgroup:", cgroupPath)
}
