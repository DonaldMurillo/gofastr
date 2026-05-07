package runtime

import (
	"embed"
	"io/fs"
)

//go:embed runtime.js
var runtimeFS embed.FS

// RuntimeJS returns the JavaScript runtime as a string.
func RuntimeJS() (string, error) {
	data, err := fs.ReadFile(runtimeFS, "runtime.js")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RuntimeSize returns the size of the runtime in bytes.
func RuntimeSize() int {
	js, err := RuntimeJS()
	if err != nil {
		return 0
	}
	return len(js)
}

// MustRuntimeJS returns the runtime JS or panics.
func MustRuntimeJS() string {
	js, err := RuntimeJS()
	if err != nil {
		panic(err)
	}
	return js
}
