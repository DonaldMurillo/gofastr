package admin

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

func TestFailedJobsMetricUsesLocale(t *testing.T) {
	assertQueueCountsUseLocale(t, queue.JobStats{"failed": 1234}, []string{"/admin", "/admin/_metric/jobs"}, ".fui-stat-card__value")
}
