package journal

import "testing"

func TestMemoryJournalOwnsPayloads(t *testing.T) {
	j := NewMemory()
	payload := []byte(`{"name":"first"}`)
	if _, err := j.Append(Entry{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	payload[9] = 'X'
	entries, err := j.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(entries[0].Payload); got != `{"name":"first"}` {
		t.Errorf("mutation after Append changed the journal to %s", got)
	}
	entries[0].Payload[9] = 'Y'
	entries, err = j.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(entries[0].Payload); got != `{"name":"first"}` {
		t.Errorf("mutation after Read changed the journal to %s", got)
	}
}
