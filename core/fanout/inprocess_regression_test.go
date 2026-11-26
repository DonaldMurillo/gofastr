package fanout

import "testing"

func TestInProcessDiscardsEmptyTopic(t *testing.T) {
	ip := NewInProcess()
	first, err := ip.Subscribe("topic", func([]byte) {})
	if err != nil {
		t.Fatal(err)
	}
	last, err := ip.Subscribe("topic", func([]byte) {})
	if err != nil {
		t.Fatal(err)
	}
	defer last()
	first()
	if len(ip.topics["topic"]) != 1 {
		t.Fatal("cancel removed a remaining subscriber")
	}
	last()
	if _, ok := ip.topics["topic"]; ok {
		t.Fatal("cancel retained the empty topic")
	}
}
