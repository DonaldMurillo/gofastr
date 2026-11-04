package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type RPCError struct {
	Code    int
	Message string
}

func (err *RPCError) Error() string { return err.Message }

func safeIndexed(output http.ResponseWriter, err error) {
	http.Error(output, fmt.Sprintf("%[1]v %[2]T", "safe", err), 500)
	http.Error(output, fmt.Sprintf("%%v %T", err), 500)
	http.Error(output, fmt.Sprintf("%[2]T %[1]s", "safe", err), 500)
	http.Error(output, fmt.Sprintf("%[1]s", "safe", err), 500)
}

func leakedIndexed(output http.ResponseWriter, err error) {
	http.Error(output, fmt.Sprintf("%[2]v %[1]T", "safe", err), 500)   // want `internal error`
	http.Error(output, fmt.Sprintf("%[1]s %v", "safe", err), 500)      // want `internal error`
	http.Error(output, fmt.Sprintf("%[2]*.[3]*[1]v", err, 10, 2), 500) // want `internal error`
}

func extraOperand(output http.ResponseWriter, err error) {
	http.Error(output, fmt.Sprintf("%v", "safe", err), 500) // want `internal error`
}

func safeJSONFormat(output http.ResponseWriter, data []byte) {
	var value any
	err := json.Unmarshal(data, &value)
	http.Error(output, fmt.Sprintf("%[1]v %[2]T", "safe", err), 400)
}

func namedDecode(output http.ResponseWriter, data []byte) {
	decode := func() (err error) {
		var value any
		err = json.Unmarshal(data, &value)
		return
	}
	err := decode()
	http.Error(output, err.Error(), 400) // want `sends encoding/json's error text to the client`
}

func identityCallback(gate func() error) *RPCError {
	identity := func(err error) error { return err }
	err := identity(gate())
	return &RPCError{Code: -32602, Message: err.Error()} // want `rebuilds a \*RPCError from an error's text`
}

func inlineIdentity(gate func() error) *RPCError {
	err := (func(err error) error { return err })(gate())
	return &RPCError{Code: -32602, Message: err.Error()} // want `rebuilds a \*RPCError from an error's text`
}

func identityDecode(output http.ResponseWriter, data []byte) {
	identity := func(err error) error { return err }
	var value any
	err := identity(json.Unmarshal(data, &value))
	http.Error(output, err.Error(), 400) // want `sends encoding/json's error text to the client`
}

func constantLiteral(gate func() error) *RPCError {
	identity := func(err error) error { return errors.New("safe") }
	err := identity(gate())
	return &RPCError{Code: -32602, Message: err.Error()}
}

func separateInvocations(gate func() error) *RPCError {
	identity := func(err error) error { return err }
	_ = identity(gate())
	err := identity(errors.New("safe"))
	return &RPCError{Code: -32602, Message: err.Error()}
}

func malformedFormat(output http.ResponseWriter, err error) {
	http.Error(output, fmt.Sprintf("%[0]v %v", err), 500)   // want `internal error`
	http.Error(output, fmt.Sprintf("%[bad]s %s", err), 500) // want `internal error`
	http.Error(output, fmt.Sprintf("%[bad %q", err), 500)   // want `internal error`
}

func variadicCallback(gate func() error) *RPCError {
	identity := func(errs ...error) error { return errors.Join(errs...) }
	err := identity(errors.New("safe"), gate())
	return &RPCError{Code: -32602, Message: err.Error()} // want `rebuilds a \*RPCError from an error's text`
}

func indexedVariadicCallback(gate func() error) *RPCError {
	identity := func(errs ...error) error { return errs[1] }
	err := identity(errors.New("safe"), gate())
	return &RPCError{Code: -32602, Message: err.Error()} // want `rebuilds a \*RPCError from an error's text`
}

func spreadVariadicCallback(gate func() error) *RPCError {
	identity := func(errs ...error) error { return errors.Join(errs...) }
	err := identity([]error{errors.New("safe"), gate()}...)
	return &RPCError{Code: -32602, Message: err.Error()} // want `rebuilds a \*RPCError from an error's text`
}

func safeVariadicCallback(gate func() error) *RPCError {
	identity := func(errs ...error) error { return errors.Join(errs...) }
	_ = identity(gate())
	err := identity(errors.New("safe"), errors.New("also safe"))
	return &RPCError{Code: -32602, Message: err.Error()}
}
