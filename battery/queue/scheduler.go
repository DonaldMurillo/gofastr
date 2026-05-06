package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ScheduledJob defines a recurring job configuration.
type ScheduledJob struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Interval time.Duration   `json:"interval"`
	NextRun  time.Time       `json:"next_run"`
}

// Scheduler enqueues recurring jobs onto one or more Queue backends.
type Scheduler struct {
	mu        sync.Mutex
	queues    []Queue
	schedules []ScheduledJob
}

// NewScheduler creates a new Scheduler that dispatches to the given queues.
func NewScheduler(queues ...Queue) *Scheduler {
	return &Scheduler{
		queues: queues,
	}
}

// Every returns a ScheduleBuilder that starts with the given interval.
func (s *Scheduler) Every(interval time.Duration) *ScheduleBuilder {
	return &ScheduleBuilder{
		scheduler: s,
		interval:  interval,
	}
}

// ScheduleBuilder provides a fluent API for building scheduled jobs.
type ScheduleBuilder struct {
	scheduler *Scheduler
	interval  time.Duration
	jobType   string
	payload   json.RawMessage
}

// Job sets the job type and payload for the scheduled job.
func (b *ScheduleBuilder) Job(jobType string, payload any) *ScheduleBuilder {
	b.jobType = jobType
	if payload != nil {
		switch v := payload.(type) {
		case json.RawMessage:
			b.payload = v
		case []byte:
			b.payload = json.RawMessage(v)
		case string:
			b.payload = json.RawMessage(v)
		default:
			data, _ := json.Marshal(payload)
			b.payload = data
		}
	}
	return b
}

// Register adds the scheduled job to the scheduler.
func (b *ScheduleBuilder) Register() {
	if b.jobType == "" {
		return
	}
	b.scheduler.mu.Lock()
	defer b.scheduler.mu.Unlock()
	b.scheduler.schedules = append(b.scheduler.schedules, ScheduledJob{
		Type:     b.jobType,
		Payload:  b.payload,
		Interval: b.interval,
		NextRun:  time.Now().Add(b.interval),
	})
}

// Start begins the scheduling loop. It blocks until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	schedules := make([]ScheduledJob, len(s.schedules))
	copy(schedules, s.schedules)
	s.mu.Unlock()

	if len(schedules) == 0 {
		return
	}

	// Use the shortest interval as the ticker duration.
	minInterval := schedules[0].Interval
	for _, sch := range schedules {
		if sch.Interval < minInterval {
			minInterval = sch.Interval
		}
	}

	ticker := time.NewTicker(minInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.dispatchDue(ctx, now)
		}
	}
}

func (s *Scheduler) dispatchDue(ctx context.Context, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.schedules {
		sch := &s.schedules[i]
		if now.After(sch.NextRun) || now.Equal(sch.NextRun) {
			job := Job{
				Type:        sch.Type,
				Payload:     sch.Payload,
				MaxAttempts: 3,
				CreatedAt:   now,
			}

			for _, q := range s.queues {
				if err := q.Enqueue(ctx, job); err != nil {
					fmt.Printf("scheduler: failed to enqueue job %q: %v\n", sch.Type, err)
				}
			}

			sch.NextRun = now.Add(sch.Interval)
		}
	}
}
