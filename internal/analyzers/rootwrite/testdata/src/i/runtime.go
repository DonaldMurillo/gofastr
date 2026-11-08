package i

import "os"

func JobDirSafe(baseDir, id string) error { return jobDirSafe(baseDir, id) }

func StageViaRoot(baseDir, name string) (*os.File, error) { return stageViaRoot(baseDir, name) }
