package errb

import "errors"

func boom() error { return errors.New("x") }

func bad() {
	if err := boom(); err != nil { // want `error branch with an empty body`
	}
}

func good() error {
	if err := boom(); err != nil {
		return err
	}
	return nil
}

func deliberate() {
	_ = boom() // ignoring on purpose reads as ignoring
}
