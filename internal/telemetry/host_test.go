package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeSampler(cpu func() (uint64, uint64, bool)) *HostSampler {
	return &HostSampler{dir: ".", cpu: cpu,
		mem:        func() (int64, int64) { return 600 << 20, 1 << 30 },
		disk:       func(string) (int64, int64) { return 20 << 30, 25 << 30 },
		rss:        func() int64 { return 80 << 20 },
		goroutines: func() int { return 42 },
		heap:       func() int64 { return 30 << 20 }}
}

// The CPU share is the busy time between two readings over all the
// time between them. The first reading has nothing to measure from, and
// counters that go backwards measure nothing.
func TestSamplerMeasuresCPUBetweenReadings(t *testing.T) {
	steps := []struct {
		busy, total uint64
		ok          bool
		want        float64
	}{
		{1000, 10000, true, -1},   // the first
		{1250, 11000, true, 0.25}, // 250 busy of 1000
		{1250, 12000, true, 0},    // idle
		{2250, 13000, true, 1},    // every core busy
		{2250, 13000, true, -1},   // no time passed
		{100, 500, true, -1},      // the counters started again
		{400, 1500, true, 0.3},    // and are measured from there
		{0, 0, false, -1},         // unreadable
		{900, 2500, true, 0.5},    // the last good one still stands
		{5000, 2600, true, -1},    // more busy than total: not a share
		{5100, 2700, true, 1},     // measured again from there
		{4000, 2800, true, -1},    // the busy counter alone went back
	}
	i := 0
	h := fakeSampler(func() (uint64, uint64, bool) { return steps[i].busy, steps[i].total, steps[i].ok })
	for i = range steps {
		got := h.Read(day0).CPU
		// Written so that a NaN, which compares false with everything,
		// fails.
		if d := got - steps[i].want; !(d >= -1e-9 && d <= 1e-9) {
			t.Fatalf("reading %d: CPU %v, want %v", i, got, steps[i].want)
		}
	}
	r := h.Read(day0)
	if r.MemPercent() != 58 || r.DiskPercent() != 80 || r.Goroutines != 42 || r.RSSBytes != 80<<20 || r.HeapBytes != 30<<20 {
		t.Fatalf("the rest of a reading: %+v (mem %d%%, disk %d%%)", r, r.MemPercent(), r.DiskPercent())
	}
	if (HostReading{}).DiskPercent() != -1 || (HostReading{MemUsed: 5}).MemPercent() != -1 || (HostReading{DiskUsed: 9, DiskTotal: 3}).DiskPercent() != 100 {
		t.Fatal("a percentage of an unknown or impossible total")
	}
}

func TestDiskFromBlocks(t *testing.T) {
	if used, total := diskFromBlocks(1000, 250, 4096); used != 750*4096 || total != 1000*4096 {
		t.Fatalf("used %d of %d", used, total)
	}
	for name, c := range map[string][3]uint64{"no blocks": {0, 0, 4096}, "no block size": {10, 1, 0}, "more free than there is": {10, 11, 4096},
		"a block size no disk has": {10, 1, 1 << 40}, "too big to count": {1 << 60, 1, 4096}} {
		if used, total := diskFromBlocks(c[0], c[1], c[2]); used != 0 || total != 0 {
			t.Errorf("%s read as %d of %d", name, used, total)
		}
	}
}

// A reading comes back from its line as it went in, with what the
// platform could not say left out; and the series puts each reading in
// its step: the mean CPU, the most memory, the last disk.
func TestHostReadingsRoundTripAndSeries(t *testing.T) {
	full := HostReading{Time: day0.Add(90 * time.Second), CPU: 0.3712, MemUsed: 600 << 20, MemTotal: 1 << 30, DiskUsed: 20 << 30, DiskTotal: 25 << 30, Goroutines: 42, HeapBytes: 30 << 20, RSSBytes: 80 << 20}
	line := appendHost(nil, full)
	if got, ok := parseHost(line[:len(line)-1]); !ok || got != full {
		t.Fatalf("read back %+v from %s", got, line)
	}
	bare := HostReading{Time: day0, CPU: -1, Goroutines: 7, HeapBytes: 1 << 20}
	if got := string(appendHost(nil, bare)); got != `{"time":"2026-11-10T00:00:00Z","goroutines":7,"heap":1048576}`+"\n" {
		t.Fatalf("a reading with little to say: %s", got)
	}
	if got, ok := parseHost([]byte(`{"time":"2026-11-10T00:00:00Z","cpu":7.5,"mem_used":-4,"goroutines":3}`)); !ok || got.CPU != -1 || got.MemUsed != 0 || got.Goroutines != 3 {
		t.Fatalf("values that cannot be: %+v", got)
	}
	if _, ok := parseHost([]byte(`not a line`)); ok {
		t.Fatal("read a line with no time")
	}

	dir := t.TempDir()
	now := day0.Add(time.Hour)
	w, err := newWriter(Config{Dir: dir}, func() time.Time { return now }, false)
	if err != nil {
		t.Fatal(err)
	}
	gb := int64(1 << 30)
	for _, r := range []HostReading{
		{Time: day0.Add(1 * time.Minute), CPU: -1, MemUsed: 1 * gb, MemTotal: 4 * gb, DiskUsed: 10 * gb, DiskTotal: 25 * gb},
		{Time: day0.Add(2 * time.Minute), CPU: 0.6, MemUsed: 3 * gb, MemTotal: 4 * gb, DiskUsed: 11 * gb, DiskTotal: 25 * gb},
		{Time: day0.Add(3 * time.Minute), CPU: 0.2, MemUsed: 2 * gb, MemTotal: 4 * gb, DiskUsed: 12 * gb, DiskTotal: 25 * gb},
		{Time: day0.Add(12 * time.Minute), CPU: 0.9, MemUsed: 1 * gb, MemTotal: 4 * gb, DiskUsed: 13 * gb, DiskTotal: 25 * gb},
		{Time: day0.Add(40 * time.Minute), CPU: 0.1, MemUsed: 1 * gb, MemTotal: 4 * gb, DiskUsed: 14 * gb, DiskTotal: 25 * gb}, // past the window
	} {
		if err := w.WriteHost(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T) {
		got, err := statsStore(t, dir).Host(day0, day0.Add(30*time.Minute), 10*time.Minute)
		if err != nil || len(got.Points) != 3 {
			t.Fatalf("%d points, %v", len(got.Points), err)
		}
		p := got.Points[0]
		if p.Samples != 3 || p.CPU < 0.399 || p.CPU > 0.401 || p.CPUMax != 0.6 || p.MemUsed != 3*gb || p.MemTotal != 4*gb || p.DiskUsed != 12*gb {
			t.Fatalf("the first step: %+v", p)
		}
		if p := got.Points[1]; p.Samples != 1 || p.CPU != 0.9 || p.DiskUsed != 13*gb {
			t.Fatalf("the second step: %+v", p)
		}
		if p := got.Points[2]; p.Samples != 0 || p.CPU != -1 || p.CPUMax != -1 || p.MemUsed != 0 || !p.Time.Equal(day0.Add(20*time.Minute)) {
			t.Fatalf("a step with no reading: %+v", p)
		}
		if !got.Latest.Time.Equal(day0.Add(12*time.Minute)) || got.Latest.DiskUsed != 13*gb {
			t.Fatalf("the latest reading in the window: %+v", got.Latest)
		}
	}
	t.Run("plain", check)
	if err := SealDay(dir, KindHost, day0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName(KindHost, day0))); !os.IsNotExist(err) {
		t.Fatal("the plain file is still there after sealing")
	}
	t.Run("sealed", check)

	s := statsStore(t, dir)
	for name, c := range map[string][3]any{
		"no from": {time.Time{}, day0, time.Minute}, "backwards": {day0, day0.Add(-time.Hour), time.Minute},
		"seconds": {day0, day0.Add(time.Hour), 90 * time.Second}, "too many": {day0, day0.Add((maxStatsPoints + 1) * time.Minute), time.Minute},
	} {
		if _, err := s.Host(c[0].(time.Time), c[1].(time.Time), c[2].(time.Duration)); err == nil {
			t.Errorf("%s: answered", name)
		}
	}
}
