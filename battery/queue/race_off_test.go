//go:build !race

package queue

// stressJobs is the soak's job count; race_on_test.go lowers it under -race.
const stressJobs = 600
