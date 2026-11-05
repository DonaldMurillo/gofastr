package scan

import (
	"fmt"
	"io"
)

const maxScanFileBytes = 8 << 20

func (e *engine) readFile(rel string) ([]byte, error) {
	file, err := e.appRoot.Open(rel)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxScanFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxScanFileBytes {
		return nil, fmt.Errorf("exceeds %d-byte scan limit", maxScanFileBytes)
	}
	return data, nil
}
