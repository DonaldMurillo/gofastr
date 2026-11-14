// Package telemetry stores an app's logs and traces as plain JSON-lines
// files, one file per kind per UTC day, and answers queries by reading
// those files directly.
//
// This is the baseline with no index: every query is a scan, narrowed by
// a binary search on the leading "time" field where a time window is
// known. Its benchmark numbers decide which queries need the sealed,
// indexed segments that come next. Nothing outside this module can
// import it; the format and API move to a public package once the
// writer is wired into battery/log.
//
// File format. logs-YYYYMMDD.jsonl holds what battery/log already
// writes: one slog JSON object per line, "time" first, plus "trace_id"
// and "span_id" attributes on lines written inside a traced request.
// traces-YYYYMMDD.jsonl holds one complete trace per line, written when
// its root span ends; AppendTrace is the only writer of that shape.
package telemetry

import (
	"errors"
	"math"
	"time"
)

// Defaults a 1 vCPU / 1 GB / 25 GB host can carry at about 20 requests
// a second. Every one is a setting; the load is not.
const (
	DefaultRetention      = 14 * 24 * time.Hour
	DefaultMaxDiskBytes   = 5 << 30
	DefaultSampleRate     = 0.05
	DefaultSlowThreshold  = 500 * time.Millisecond
	DefaultMaxMemoryBytes = 32 << 20
)

// Config holds the store's settings. The zero value of a field means
// its default; Resolve fills those in and refuses values that make no
// sense. Dir is read by Open today. The rest describe what the writer
// keeps (SampleRate, SlowThreshold) and the budgets it and the sealer
// will enforce (Retention, MaxDiskBytes, MaxMemoryBytes); until that
// lands they drive the generator and the benchmark's pass marks.
type Config struct {
	// Dir is the directory holding the log and trace files.
	Dir string
	// Retention is how long a day's files are kept.
	Retention time.Duration
	// MaxDiskBytes caps the directory's total size; the oldest files go
	// first when it is reached, before Retention if need be.
	MaxDiskBytes int64
	// SampleRate is the share of ordinary traces kept, from 0 to 1.
	// Error and slow traces are always kept. Zero means the default;
	// set KeepNoOrdinary to keep none.
	SampleRate float64
	// KeepNoOrdinary keeps only error and slow traces.
	KeepNoOrdinary bool
	// SlowThreshold is the duration at which a trace counts as slow.
	SlowThreshold time.Duration
	// MaxMemoryBytes caps what the writer buffers; past it, data is
	// dropped and counted, never queued.
	MaxMemoryBytes int64
}

// Resolve returns c with defaults filled in, or an error naming the
// first setting that is out of range.
func (c Config) Resolve() (Config, error) {
	switch {
	case c.Dir == "":
		return c, errors.New("telemetry: Dir is required")
	case c.Retention < 0:
		return c, errors.New("telemetry: Retention is negative")
	case c.MaxDiskBytes < 0:
		return c, errors.New("telemetry: MaxDiskBytes is negative")
	case math.IsNaN(c.SampleRate) || c.SampleRate < 0 || c.SampleRate > 1:
		return c, errors.New("telemetry: SampleRate must be between 0 and 1")
	case c.SlowThreshold < 0:
		return c, errors.New("telemetry: SlowThreshold is negative")
	case c.MaxMemoryBytes < 0:
		return c, errors.New("telemetry: MaxMemoryBytes is negative")
	}
	if c.Retention == 0 {
		c.Retention = DefaultRetention
	}
	if c.MaxDiskBytes == 0 {
		c.MaxDiskBytes = DefaultMaxDiskBytes
	}
	if c.SampleRate == 0 {
		c.SampleRate = DefaultSampleRate
	}
	if c.KeepNoOrdinary {
		c.SampleRate = 0
	}
	if c.SlowThreshold == 0 {
		c.SlowThreshold = DefaultSlowThreshold
	}
	if c.MaxMemoryBytes == 0 {
		c.MaxMemoryBytes = DefaultMaxMemoryBytes
	}
	return c, nil
}
