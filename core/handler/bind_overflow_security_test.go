package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBind_IntegerOverflowRejected verifies that Bind rejects integer
// query/header/path values that do not fit the destination field's bit
// width. Previously setField parsed every int as int64 / uint as uint64
// and then SetInt/SetUint truncated to the field width silently:
// ?small=300 into an int8 bound 44 (300 & 0xff) with a nil error.
// Attack: silent truncation changes a value's meaning (300 limit → 44).
func TestBind_IntegerOverflowRejected(t *testing.T) {
	type ints struct {
		Small  int8   `query:"small"`
		Mid    int16  `query:"mid"`
		Wide32 int32  `query:"wide32"`
		Wide   int64  `query:"wide"`
		Nat    uint   `query:"nat"`
		U8     uint8  `query:"u8"`
		U16    uint16 `query:"u16"`
		U32    uint32 `query:"u32"`
		U64    uint64 `query:"u64"`
	}
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"int8 in range max", "?small=127", false},
		{"int8 in range min", "?small=-128", false},
		{"int8 over range", "?small=300", true},
		{"int8 under range", "?small=-129", true},
		{"int16 over range", "?mid=40000", true},
		{"int32 over range", "?wide32=5000000000", true},
		{"uint negative", "?u8=-1", true},
		{"uint8 over range", "?u8=256", true},
		{"uint16 over range", "?u16=70000", true},
		{"uint32 over range", "?u32=5000000000", true},
		{"int64 in range max", "?wide=9223372036854775807", false},
		{"uint64 in range max", "?u64=18446744073709551615", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dst ints
			req := httptest.NewRequest(http.MethodGet, "/"+tt.query, nil)
			err := Bind(req, &dst)
			if tt.wantErr {
				if err == nil {
					t.Errorf("SECURITY: [bind] Bind accepted out-of-range %q silently (truncated). Attack: silent integer truncation changes value semantics.", tt.query)
				}
			} else {
				if err != nil {
					t.Errorf("in-range %q wrongly rejected: %v", tt.query, err)
				}
			}
		})
	}
}

// TestBind_IntegerOverflowErrorIs400 verifies the truncation error is a
// 400 (bad request) at the adapter, not a silent 200 or a 500.
func TestBind_IntegerOverflowErrorIs400(t *testing.T) {
	var dst struct {
		Small int8 `query:"small"`
	}
	req := httptest.NewRequest(http.MethodGet, "/?small=300", nil)
	err := Bind(req, &dst)
	if err == nil {
		t.Fatalf("SECURITY: [bind] Bind accepted ?small=300 into int8. Attack: silent truncation.")
	}
	var he *Error
	if !errAsBind(err, &he) || he.Code != http.StatusBadRequest {
		t.Errorf("expected a 400 *handler.Error, got %v", err)
	}
}

// errAsBind is a tiny errors.As shim to avoid importing errors here.
func errAsBind(err error, target **Error) bool {
	for e := err; e != nil; {
		if he, ok := e.(*Error); ok {
			*target = he
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// TestBind_Float32OverflowRejected is the float sweep of the same bug
// shape: ParseFloat previously used bitsize 64 even for float32 fields,
// so a value overflowing float32 range silently became ±Inf instead of
// a parse error. Now ParseFloat uses the field's bit width.
func TestBind_Float32OverflowRejected(t *testing.T) {
	var dst struct {
		F32 float32 `query:"f32"`
	}
	req := httptest.NewRequest(http.MethodGet, "/?f32=1e40", nil)
	if err := Bind(req, &dst); err == nil {
		t.Errorf("SECURITY: [bind] Bind accepted float32-overflow 1e40 silently (→ +Inf). Attack: silent range overflow.")
	}
}
