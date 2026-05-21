package log

import "testing"

func TestRingSinkAppendsUnderCap(t *testing.T) {
	r := NewRingSink(3)
	for i := byte('A'); i <= byte('C'); i++ {
		if err := r.Write([]byte{i}); err != nil {
			t.Fatal(err)
		}
	}
	got := r.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range []byte{'A', 'B', 'C'} {
		if got[i][0] != want {
			t.Errorf("got[%d]=%q want %q", i, got[i], want)
		}
	}
}

func TestRingSinkWrapsAtCap(t *testing.T) {
	r := NewRingSink(3)
	for i := byte('A'); i <= byte('E'); i++ {
		_ = r.Write([]byte{i})
	}
	got := r.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (oldest dropped on wrap)", len(got))
	}
	// After A,B,C,D,E with cap=3, we should see C,D,E.
	for i, want := range []byte{'C', 'D', 'E'} {
		if got[i][0] != want {
			t.Errorf("got[%d]=%q want %q (chronological after wrap)", i, got[i], want)
		}
	}
}

func TestRingSinkWriteAfterClose(t *testing.T) {
	r := NewRingSink(2)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Write([]byte("x")); err != ErrSinkClosed {
		t.Fatalf("Write after Close = %v, want ErrSinkClosed", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal("Close not idempotent")
	}
}

func TestRingSinkSnapshotDecoded(t *testing.T) {
	r := NewRingSink(4)
	_ = r.Write([]byte(`{"msg":"a","level":"INFO"}`))
	_ = r.Write([]byte(`{"msg":"b","level":"ERROR"}`))
	_ = r.Write([]byte(`not json`))
	_ = r.Write([]byte(`{"msg":"c","level":"INFO"}`))

	got := r.SnapshotDecoded()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (non-JSON entry skipped)", len(got))
	}
	if got[0]["msg"] != "a" || got[2]["msg"] != "c" {
		t.Errorf("decoded entries out of order: %v", got)
	}
}
