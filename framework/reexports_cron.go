package framework

import "github.com/DonaldMurillo/gofastr/framework/cron"

// Root spellings of framework/cron.

type (
	CronJob   = cron.CronJob
	Scheduler = cron.Scheduler
)

func NewScheduler() *cron.Scheduler { return cron.NewScheduler() }
