package project

// GetEnvSource is the environment fallback helper emitted into scaffolded main.go files.
const GetEnvSource = `func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
`
