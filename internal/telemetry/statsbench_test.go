package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Overview's widest read: seven days of thirty routes, every day
// but the last sealed.
func BenchmarkStatsSevenDays(b *testing.B) {
	dir := b.TempDir()
	const days, routes = 7, 30
	for d := range days {
		day := day0.Add(time.Duration(d) * 24 * time.Hour)
		r := NewRollup()
		var file []byte
		for m := range 24 * 60 {
			at := day.Add(time.Duration(m) * time.Minute)
			for rt := range routes {
				for k := range 1 + (m+rt)%3 {
					r.Observe(at, fmt.Sprintf("GET /api/resource%02d/{id}", rt), time.Duration(5+k*40+rt)*time.Millisecond, k == 2 && rt == 0, false)
				}
			}
			file = append(file, r.Take(at.Add(time.Minute), true)...)
		}
		if err := os.WriteFile(filepath.Join(dir, FileName(KindStats, day)), file, 0o600); err != nil {
			b.Fatal(err)
		}
		if d < days-1 {
			if err := SealDay(dir, KindStats, day); err != nil {
				b.Fatal(err)
			}
		}
	}
	var size int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			size += info.Size()
		}
	}
	s, err := Open(Config{Dir: dir})
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	q := StatsQuery{From: day0, To: day0.Add(days * 24 * time.Hour), Step: 2 * time.Hour}
	b.ReportMetric(float64(size)/1e6, "MB-on-disk")
	for b.Loop() {
		got, err := s.Stats(q)
		if err != nil || len(got.Routes) != routes {
			b.Fatalf("%d routes, %v", len(got.Routes), err)
		}
	}
}
