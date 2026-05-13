package framework

import "github.com/DonaldMurillo/gofastr/framework/cron"

// Re-exports of framework/cron so callers using framework.X (benchmarks,
// example apps) keep compiling after the cron package extraction.

type (
	CronJob   = cron.CronJob
	Scheduler = cron.Scheduler
)

var NewScheduler = cron.NewScheduler
