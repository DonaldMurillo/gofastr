package telemetry

import (
	"testing"
	"time"
)

func TestProcFilesAreReadAsTheKernelWritesThem(t *testing.T) {
	const stat = "cpu  215180 3833 58180 285662 81552 0 5153 3208 900 0\ncpu0 54423 619 14617 71255 20412 0 1841 776 0 0\n"
	busy, total, ok := parseProcStat([]byte(stat))
	// user+nice+system+irq+softirq+steal busy; idle and iowait not; the
	// guest fields past steal are in user already.
	if !ok || busy != 215180+3833+58180+0+5153+3208 || total != busy+285662+81552 {
		t.Fatalf("busy %d, total %d, %v", busy, total, ok)
	}
	for name, bad := range map[string]string{"empty": "", "another line first": "intr 1 2 3 4 5\n", "too few": "cpu 1 2 3\n", "not numbers": "cpu a b c d e\n", "per-core only": "cpu0 1 2 3 4 5\n", "all zero": "cpu 0 0 0 0 0\n"} {
		if _, _, ok := parseProcStat([]byte(bad)); ok {
			t.Errorf("parseProcStat read %s", name)
		}
	}

	const meminfo = "MemTotal:       16472076 kB\nMemFree:          200000 kB\nMemAvailable:   15779728 kB\nSwapTotal:             0 kB\n"
	if used, total := parseMeminfo([]byte(meminfo)); total != 16472076<<10 || used != (16472076-15779728)<<10 {
		t.Fatalf("meminfo: used %d of %d", used, total)
	}
	for name, bad := range map[string]string{
		"empty": "", "no available": "MemTotal: 100 kB\n", "no total": "MemAvailable: 100 kB\n",
		"a key that only ends like it": "XMemTotal: 100 kB\nXMemAvailable: 50 kB\n", "not a number": "MemTotal: lots kB\nMemAvailable: 1 kB\n",
	} {
		if used, total := parseMeminfo([]byte(bad)); used != 0 || total != 0 {
			t.Errorf("parseMeminfo read %s as %d of %d", name, used, total)
		}
	}
	// More available than there is reads as nothing used, not negative.
	if used, total := parseMeminfo([]byte("MemTotal: 100 kB\nMemAvailable: 900 kB\n")); used != 0 || total != 100<<10 {
		t.Fatalf("available past total: %d of %d", used, total)
	}

	if got := parseStatm([]byte("715 408 380 5 0 125 0\n"), 4096); got != 408*4096 {
		t.Fatalf("statm: %d", got)
	}
	for _, bad := range []string{"", "715", "715 x", "715 -3"} {
		if got := parseStatm([]byte(bad), 4096); got != 0 {
			t.Errorf("parseStatm(%q) = %d", bad, got)
		}
	}

	// And the machine this runs on answers with something a machine
	// could be.
	// One reading is enough: the sampler noted the counters when it
	// was made.
	h := NewHostSampler(t.TempDir())
	time.Sleep(40 * time.Millisecond)
	r := h.Read(time.Now())
	if r.CPU < 0 || r.CPU > 1 || r.MemTotal <= 0 || r.MemUsed <= 0 || r.MemUsed > r.MemTotal || r.DiskTotal <= 0 || r.DiskUsed > r.DiskTotal ||
		r.RSSBytes <= 0 || r.HeapBytes <= 0 || r.Goroutines < 1 {
		t.Fatalf("a reading of this machine: %+v", r)
	}
}
