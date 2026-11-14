package telemetry

import (
	"bytes"
	"errors"
	"runtime"
	"strconv"
	"time"
)

// HostReading is one look at the machine and at this process. A field
// the platform cannot give is zero, and CPU is negative: the first
// reading has no earlier one to measure from, and only Linux says.
type HostReading struct {
	Time time.Time
	// CPU is the share of all cores that were busy since the reading
	// before, from 0 to 1.
	CPU float64
	// MemUsed is the memory programs hold and the kernel cannot hand
	// back: total less what it reports as available.
	MemUsed, MemTotal int64
	// DiskUsed and DiskTotal are of the filesystem the telemetry
	// directory is on. Space only root may use counts as used.
	DiskUsed, DiskTotal int64
	// Goroutines, HeapBytes and RSSBytes are this process's: its
	// goroutines, the Go heap in use, and the memory it holds.
	Goroutines          int
	HeapBytes, RSSBytes int64
}

// DiskPercent is how full the disk is, 0 to 100, or -1 when unknown.
func (r HostReading) DiskPercent() int {
	return percentOf(r.DiskUsed, r.DiskTotal)
}

// MemPercent is how much of memory is used, 0 to 100, or -1 when
// unknown.
func (r HostReading) MemPercent() int {
	return percentOf(r.MemUsed, r.MemTotal)
}

func percentOf(used, total int64) int {
	if total <= 0 || used < 0 {
		return -1
	}
	return int(min(used, total) * 100 / total)
}

// HostSampler reads the machine. It remembers the CPU counters of its
// last reading, since a share of time is a difference of two.
type HostSampler struct {
	dir         string
	busy, total uint64
	haveCPU     bool
	cpu         func() (busy, total uint64, ok bool)
	mem         func() (used, total int64)
	disk        func(dir string) (used, total int64)
	rss         func() int64
	goroutines  func() int
	heap        func() int64
}

// NewHostSampler returns a sampler whose disk reading is of the
// filesystem holding dir. It notes the CPU counters as it is made, so
// the first reading already measures the time since then.
func NewHostSampler(dir string) *HostSampler {
	h := newHostSampler(dir)
	h.busy, h.total, h.haveCPU = h.cpu()
	return h
}

func newHostSampler(dir string) *HostSampler {
	return &HostSampler{dir: dir, cpu: hostCPU, mem: hostMem, disk: hostDisk, rss: hostRSS,
		goroutines: runtime.NumGoroutine,
		heap: func() int64 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return clampInt64(m.HeapInuse)
		}}
}

func clampInt64(v uint64) int64 {
	if v > 1<<62 {
		return 1 << 62
	}
	return int64(v)
}

// Read takes one reading. It is not safe for two callers at once.
func (h *HostSampler) Read(now time.Time) HostReading {
	r := HostReading{Time: now, CPU: -1, Goroutines: h.goroutines(), HeapBytes: h.heap(), RSSBytes: h.rss()}
	r.MemUsed, r.MemTotal = h.mem()
	r.DiskUsed, r.DiskTotal = h.disk(h.dir)
	if busy, total, ok := h.cpu(); ok {
		// Counters that went backwards (a restarted container, a
		// wrapped counter), or that claim more busy time than time
		// passed, measure nothing: start again from here. A busy
		// counter that went back alone makes the unsigned difference
		// enormous, which the last test refuses.
		if h.haveCPU && total > h.total && busy-h.busy <= total-h.total {
			r.CPU = float64(busy-h.busy) / float64(total-h.total)
		}
		h.busy, h.total, h.haveCPU = busy, total, true
	}
	return r
}

// appendHost writes one reading as a line, "time" first as every day
// file's lines have it. A field with nothing to say is left out.
func appendHost(b []byte, r HostReading) []byte {
	b = append(b, `{"time":"`...)
	b = r.Time.UTC().AppendFormat(b, time.RFC3339)
	b = append(b, '"')
	if r.CPU >= 0 {
		b = append(b, `,"cpu":`...)
		b = strconv.AppendFloat(b, r.CPU, 'f', 4, 64)
	}
	for _, f := range []struct {
		key string
		v   int64
	}{
		{"mem_used", r.MemUsed}, {"mem_total", r.MemTotal}, {"disk_used", r.DiskUsed}, {"disk_total", r.DiskTotal},
		{"goroutines", int64(r.Goroutines)}, {"heap", r.HeapBytes}, {"rss", r.RSSBytes},
	} {
		if f.v > 0 {
			b = append(b, `,"`...)
			b = append(b, f.key...)
			b = append(b, `":`...)
			b = strconv.AppendInt(b, f.v, 10)
		}
	}
	return append(b, "}\n"...)
}

// parseHost reads a line appendHost wrote. A value that is negative or
// not a number reads as unknown.
func parseHost(line []byte) (HostReading, bool) {
	at, ok := lineTime(line)
	if !ok {
		return HostReading{}, false
	}
	r := HostReading{Time: at, CPU: -1}
	if i := bytes.Index(line, []byte(`"cpu":`)); i >= 0 {
		rest := line[i+len(`"cpu":`):]
		j := 0
		for j < len(rest) && (rest[j] == '.' || (rest[j] >= '0' && rest[j] <= '9')) {
			j++
		}
		if v, err := strconv.ParseFloat(string(rest[:j]), 64); err == nil && v >= 0 && v <= 1 {
			r.CPU = v
		}
	}
	get := func(key string) int64 {
		v, ok := intField(line, key)
		if !ok || v < 0 {
			return 0
		}
		return v
	}
	r.MemUsed, r.MemTotal = get(`"mem_used":`), get(`"mem_total":`)
	r.DiskUsed, r.DiskTotal = get(`"disk_used":`), get(`"disk_total":`)
	r.Goroutines = int(min(get(`"goroutines":`), 1<<30))
	r.HeapBytes, r.RSSBytes = get(`"heap":`), get(`"rss":`)
	return r, true
}

// WriteHost appends one reading to the day's host file.
func (w *Writer) WriteHost(r HostReading) error {
	_, err := w.write(&w.host, appendHost(nil, r))
	return err
}

// HostPoint is the readings of one step of a series.
type HostPoint struct {
	Time time.Time
	// Samples is the readings that fell in the step; with none, the
	// other fields say nothing.
	Samples int
	// CPU is the mean of the step's readings and CPUMax the highest of
	// them; both are negative when no reading had one.
	CPU, CPUMax float64
	// MemUsed is the most any reading in the step saw; DiskUsed is the
	// step's last.
	MemUsed, MemTotal   int64
	DiskUsed, DiskTotal int64
}

// HostSeries is the machine over a window.
type HostSeries struct {
	// Latest is the newest reading in the window; its Time is zero
	// when there is none.
	Latest HostReading
	Points []HostPoint
}

// Host returns the machine's readings over [from, to) in steps of
// whole minutes, as Stats does for requests.
func (s *Store) Host(from, to time.Time, step time.Duration) (HostSeries, error) {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return HostSeries{}, errors.New("telemetry: host readings need a From before a To")
	}
	if step < time.Minute || step%time.Minute != 0 {
		return HostSeries{}, errors.New("telemetry: host step must be whole minutes")
	}
	from = from.UTC().Truncate(step)
	steps := to.Sub(from) / step
	if to.Sub(from)%step != 0 {
		steps++
	}
	if steps > maxStatsPoints {
		return HostSeries{}, errors.New("telemetry: host window has too many steps")
	}
	out := HostSeries{Points: make([]HostPoint, steps)}
	cpuSum := make([]float64, steps)
	cpuN := make([]int, steps)
	for i := range out.Points {
		out.Points[i].Time, out.Points[i].CPU, out.Points[i].CPUMax = from.Add(time.Duration(i)*step), -1, -1
	}
	err := s.scan(KindHost, from, to, false, false, func(line []byte) bool {
		r, ok := parseHost(line)
		if !ok || r.Time.Before(from) || !r.Time.Before(to) {
			return true
		}
		i := r.Time.Sub(from) / step
		p := &out.Points[i]
		p.Samples++
		if r.CPU >= 0 {
			cpuSum[i] += r.CPU
			cpuN[i]++
			p.CPUMax = max(p.CPUMax, r.CPU)
		}
		if r.MemUsed > p.MemUsed {
			p.MemUsed = r.MemUsed
		}
		if r.MemTotal > 0 {
			p.MemTotal = r.MemTotal
		}
		if r.DiskTotal > 0 {
			p.DiskUsed, p.DiskTotal = r.DiskUsed, r.DiskTotal
		}
		if !r.Time.Before(out.Latest.Time) {
			out.Latest = r
		}
		return true
	})
	if err != nil {
		return HostSeries{}, err
	}
	for i := range out.Points {
		if cpuN[i] > 0 {
			out.Points[i].CPU = cpuSum[i] / float64(cpuN[i])
		}
	}
	return out, nil
}

// diskFromBlocks turns a filesystem's block counts into bytes. Numbers
// that cannot be a disk (no blocks, more free than there are, a size
// past what an int64 of bytes holds) read as unknown.
func diskFromBlocks(blocks, avail, bsize uint64) (used, total int64) {
	if blocks == 0 || bsize == 0 || avail > blocks || bsize > 1<<30 || blocks > (1<<62)/bsize {
		return 0, 0
	}
	return int64((blocks - avail) * bsize), int64(blocks * bsize)
}
