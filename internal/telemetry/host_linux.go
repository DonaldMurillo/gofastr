package telemetry

import (
	"bytes"
	"os"
	"strconv"
)

// readSmall reads a /proc file, which is small and never worth more
// than a page or two.
func readSmall(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 16<<10)
	n, err := f.Read(buf)
	if err != nil {
		// Nothing read is nothing known; the parsers say so.
		return nil
	}
	return buf[:n]
}

func hostCPU() (busy, total uint64, ok bool) { return parseProcStat(readSmall("/proc/stat")) }

// parseProcStat reads the first line of /proc/stat, the time every
// core has spent in each state since boot: user, nice, system, idle,
// iowait, irq, softirq, steal. Busy is all of it but idle and iowait.
func parseProcStat(b []byte) (busy, total uint64, ok bool) {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	fields := bytes.Fields(line)
	if len(fields) < 5 || string(fields[0]) != "cpu" {
		return 0, 0, false
	}
	// Past steal come guest times, which user and nice already hold.
	for i, f := range fields[1:min(len(fields), 9)] {
		v, err := strconv.ParseUint(string(f), 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += v
		if i != 3 && i != 4 {
			busy += v
		}
	}
	return busy, total, total > 0
}

func hostMem() (used, total int64) { return parseMeminfo(readSmall("/proc/meminfo")) }

// parseMeminfo reads MemTotal and MemAvailable, in kB. Available is the
// kernel's own estimate of what a new program could have without
// swapping, which is the number that says whether the machine is short.
func parseMeminfo(b []byte) (used, total int64) {
	kb := func(key string) int64 {
		i := bytes.Index(b, []byte(key))
		if i < 0 || (i > 0 && b[i-1] != '\n') {
			return -1
		}
		fields := bytes.Fields(b[i+len(key):])
		if len(fields) == 0 {
			return -1
		}
		v, err := strconv.ParseInt(string(fields[0]), 10, 64)
		if err != nil || v < 0 || v > 1<<50 {
			return -1
		}
		return v << 10
	}
	total, avail := kb("MemTotal:"), kb("MemAvailable:")
	if total <= 0 || avail < 0 {
		return 0, 0
	}
	return total - min(avail, total), total
}

func hostRSS() int64 { return parseStatm(readSmall("/proc/self/statm"), int64(os.Getpagesize())) }

// parseStatm reads the second field of /proc/self/statm: the pages this
// process holds in memory.
func parseStatm(b []byte, page int64) int64 {
	fields := bytes.Fields(b)
	if len(fields) < 2 || page <= 0 {
		return 0
	}
	v, err := strconv.ParseInt(string(fields[1]), 10, 64)
	if err != nil || v < 0 || v > 1<<40 {
		return 0
	}
	return v * page
}
