//go:build race

package queue

// stressJobs is the soak's job count. A race-built binary runs the soak
// about ten times slower, so it drives a third of the jobs and stays well
// inside stressDeadline.
const stressJobs = 200
