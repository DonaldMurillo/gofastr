//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActivationURLBoundary(t *testing.T) {
	for _, raw := range []string{"notes://a/x?x=%2f#frag", "NOTES://a/x", "notes://"} {
		if !validActivationURL(raw, "notes") {
			t.Errorf("refused %q", raw)
		}
	}
	for _, raw := range []string{"https://a/x", "", "notes://a/\x00x", "notes://a/\xff", "notes://a/\n", "notes://a/" + strings.Repeat("x", 2048)} {
		if validActivationURL(raw, "notes") {
			t.Errorf("accepted unsafe %q", raw)
		}
	}
	exact := "notes://" + strings.Repeat("a", activationURLLimit-len("notes://"))
	if !validActivationURL(exact, "notes") {
		t.Fatal("refused exact byte limit")
	}
	if validActivationURL(exact+"a", "notes") {
		t.Fatal("accepted over byte limit")
	}
	for _, s := range []string{"", "Notes", "1notes", "note_s", strings.Repeat("a", 33)} {
		if validActivationScheme(s) {
			t.Errorf("accepted scheme %q", s)
		}
	}
	for _, s := range []string{"n", "notes", "notes+test-v1.x"} {
		if !validActivationScheme(s) {
			t.Errorf("refused scheme %q", s)
		}
	}
}
func TestActivationIdentityAndArgBound(t *testing.T) {
	if activationIdentity("1.example") != activationIdentity("1.example") {
		t.Fatal("unstable identity")
	}
	if activationIdentity("1.example") == activationIdentity("2.example") {
		t.Fatal("identity collision")
	}
	if !strings.HasPrefix(activationIdentity(".app"), "dev.gofastr.Desktop.i") {
		t.Fatal("identity is not a valid bus name")
	}
	args := []string{"--serve", "https://wrong"}
	for i := 0; i < 20; i++ {
		args = append(args, fmt.Sprintf("notes://q/%d", i))
	}
	got := activationURLs(args, "notes")
	if len(got) != 16 || got[0] != "notes://q/4" || got[15] != "notes://q/19" {
		t.Fatalf("bounded argv=%q", got)
	}
}
func TestActivationDeliveryBeforeAttachAndBlockedCallback(t *testing.T) {
	var d activationDelivery
	for i := 0; i < 20; i++ {
		d.enqueue(fmt.Sprint(i))
	}
	entered := make(chan struct{})
	unblock := make(chan struct{})
	finished := make(chan struct{})
	var mu sync.Mutex
	var got []string
	d.attach(func(raw string) {
		mu.Lock()
		got = append(got, raw)
		n := len(got)
		mu.Unlock()
		if n == 1 {
			close(entered)
			<-unblock
		}
		if raw == "last" {
			close(finished)
		}
	}, nil)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("delivery never started")
	}
	// There is one active callback, plus at most 16 waiting deliveries.
	for i := 0; i < 20; i++ {
		d.enqueue(fmt.Sprintf("later-%d", i))
	}
	d.enqueue("last")
	d.mu.Lock()
	n := len(d.pending)
	d.mu.Unlock()
	if n != 16 {
		t.Fatalf("pending=%d", n)
	}
	close(unblock)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("delivery stalled")
	}
	d.close()
	d.enqueue("after-close")
	mu.Lock()
	defer mu.Unlock()
	want := []string{"4"}
	for i := 5; i < 20; i++ {
		want = append(want, fmt.Sprintf("later-%d", i))
	}
	want = append(want, "last")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery=%q want=%q", got, want)
	}
}
func TestActivationDisabledIsLazy(t *testing.T) {
	shell := New().(*linuxShell)
	handled, urls, err := shell.PrepareLaunch("", "ignored title", "")
	if handled || urls != nil || err != nil || shell.activation != nil {
		t.Fatalf("disabled launch=%v %v %v", handled, urls, err)
	}
	shell.ReleaseLaunch()
	shell.ReleaseLaunch()
}

func TestActivationCallbackPanicDoesNotStopDelivery(t *testing.T) {
	var d activationDelivery
	focused := make(chan struct{})
	d.attach(func(string) { panic("fixture") }, func() { close(focused) })
	d.enqueue("notes://panic")
	d.enqueue("")
	select {
	case <-focused:
	case <-time.After(time.Second):
		t.Fatal("panic stopped delivery")
	}
	d.close()
}

func TestActivationSessionAddressBoundary(t *testing.T) {
	for _, address := range []string{
		"unix:path=/run/user/1000/bus",
		"unix:abstract=/tmp/dbus-AbC+09",
		"unix:path=/tmp/a%20b%3b%2c%25+socket,guid=0123456789abcdef0123456789abcdef",
		"unix:%70ath=/tmp/socket;unix:abstract=other%3baddress",
	} {
		if !validActivationAddress(address) {
			t.Errorf("refused valid local address %q", address)
		}
	}
	for _, address := range []string{
		"", "autolaunch:", "tcp:host=localhost,port=1", "unix:path=/tmp/a;autolaunch:",
		"unix:path=/tmp/a;tcp:host=localhost,port=1", "unix:path=/tmp/a;", ";unix:path=/tmp/a",
		"UNIX:path=/tmp/a", "unix:", "unix:path", "unix:path=", "unix:abstract=",
		"unix:path=relative", "unix:path=/tmp/%zz", "unix:path=/tmp/%00hidden", "unix:path=/tmp/\x00hidden",
		"unix:path=/tmp/a,abstract=a", "unix:path=/tmp/a,path=/tmp/b", "unix:path=/tmp/a,%70ath=/tmp/b",
		"unix:dir=/tmp", "unix:tmpdir=/tmp", "unix:path=/tmp/a,unknown=x", "unix:path=/tmp/a,guid=invalid",
		"unix:path=/tmp/\xff", "unix:abstract=" + strings.Repeat("a", 4096),
	} {
		if validActivationAddress(address) {
			t.Errorf("accepted unsupported or malformed address %q", address)
		}
	}
}
