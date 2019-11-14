package agent

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
)

// List of LXC filesystem files
const (
	MemFile       string = "/proc/meminfo"
	CpuFile       string = "/proc/cpuinfo"
	UpTimeFile    string = "/proc/uptime"
	SwapsFile     string = "/proc/swaps"
	StatFile      string = "/proc/stat"
	DiskStatsFile string = "/proc/diskstats"
	LoadavgFile   string = "/proc/loadavg"
)

var (
	// IsLxcfsEnabled means whether to enable lxcfs
	LxcfsEnabled bool

	// LxcfsRootDir
	LxcfsRootDir = "/var/lib/lxc"

	// LxcfsHomeDir means /var/lib/lxc/lxcfs
	LxcfsHomeDir = "/var/lib/lxc/lxcfs"

	// LxcfsFiles is a list of LXC files
	LxcfsProcFiles = []string{MemFile, CpuFile, UpTimeFile, SwapsFile, StatFile, DiskStatsFile, LoadavgFile}
)

// CheckLxcfsMount check if the the mount point of lxcfs exists
func CheckLxcfsMount() error {
	isMount := false
	f, err := os.Open("/proc/1/mountinfo")
	if err != nil {
		return fmt.Errorf("Check lxcfs mounts failed: %v", err)
	}
	fr := bufio.NewReader(f)
	for {
		line, err := fr.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("Check lxcfs mounts failed: %v", err)
		}

		if bytes.Contains(line, []byte(LxcfsHomeDir)) {
			isMount = true
			break
		}
	}
	if !isMount {
		return fmt.Errorf("%s is not a mount point, please run \" lxcfs %s \" before debug", LxcfsHomeDir, LxcfsHomeDir)
	}
	return nil
}
