package ids

import "testing"

func TestNewIDsAreValid(t *testing.T) {
	if !ValidSession(NewSessionID()) {
		t.Error("NewSessionID not valid")
	}
	if !ValidLog(NewLogID()) {
		t.Error("NewLogID not valid")
	}
	if !ValidCall(NewCallID()) {
		t.Error("NewCallID not valid")
	}
	if !ValidJTI(NewJTI()) {
		t.Error("NewJTI not valid")
	}
	if !ValidClient(NewClientID()) {
		t.Error("NewClientID not valid")
	}
}

func TestParseRejectsWrongPrefix(t *testing.T) {
	s := string(NewLogID()) // log_…
	if _, err := ParseSession(s); err == nil {
		t.Error("ParseSession accepted log_ prefix")
	}
}
