package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

type taskScheduler struct{}

func (taskScheduler) kind() string { return "Task Scheduler" }

func (taskScheduler) taskXML(b *Battery, args []string, account string) string {
	esc := func(s string) string {
		var buf bytes.Buffer
		_ = xml.EscapeText(&buf, []byte(s))
		return buf.String()
	}
	exe, args := args[0], args[1:]
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteWindowsArg(a)
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>` + esc(b.cfg.Name+" ("+b.cfg.ID+")") + `</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + esc(account) + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + esc(account) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <!-- Retries a launch that fails. A daemon that starts and then
         crashes is not a failed launch: it stays down until next logon.
         Count is an unsignedByte, so 255 is its ceiling. -->
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>255</Count>
    </RestartOnFailure>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + esc(exe) + `</Command>
      <Arguments>` + esc(strings.Join(quoted, " ")) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

func encodeUTF16(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(u))
	out[0], out[1] = 0xFF, 0xFE
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

func quoteWindowsArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var sb strings.Builder
	sb.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			sb.WriteString(strings.Repeat(`\`, slashes+1))
			slashes = 0
		default:
			slashes = 0
		}
		sb.WriteByte(c)
	}
	sb.WriteString(strings.Repeat(`\`, slashes))
	sb.WriteByte('"')
	return sb.String()
}

var currentAccount = func() (name, sid string, err error) {
	u, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("service: resolve current user: %w", err)
	}
	return u.Username, u.Uid, nil
}

func (taskScheduler) taskName(b *Battery) (string, error) {
	_, sid, err := currentAccount()
	if err != nil {
		return "", err
	}
	return b.cfg.ID + "-" + sid, nil
}

func (t taskScheduler) install(b *Battery, exe string, port int) error {
	account, _, err := currentAccount()
	if err != nil {
		return err
	}
	name, err := t.taskName(b)
	if err != nil {
		return err
	}
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	if err := inst.ensure(); err != nil {
		return err
	}
	args, err := b.daemonArgv(exe, port)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(inst.dir, "task-*.xml")
	if err != nil {
		return fmt.Errorf("service: write task definition: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(encodeUTF16(t.taskXML(b, args, account))); err != nil {
		f.Close()
		return fmt.Errorf("service: write task definition: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("service: write task definition: %w", err)
	}
	if err := t.stop(b); err != nil {
		return err
	}
	if err := b.run("schtasks", "/Create", "/TN", name, "/XML", f.Name(), "/F"); err != nil {
		return err
	}
	return b.run("schtasks", "/Run", "/TN", name)
}

func (t taskScheduler) uninstall(b *Battery) error {
	if err := t.stop(b); err != nil {
		return err
	}
	ok, err := t.installed(b)
	if err != nil || !ok {
		return err
	}
	name, err := t.taskName(b)
	if err != nil {
		return err
	}
	return b.run("schtasks", "/Delete", "/TN", name, "/F")
}

func (t taskScheduler) start(b *Battery) error {
	name, err := t.taskName(b)
	if err != nil {
		return err
	}
	return b.run("schtasks", "/Run", "/TN", name)
}

func (t taskScheduler) stop(b *Battery) error {
	name, err := t.taskName(b)
	if err != nil {
		return err
	}
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	resp, err := callControl(inst.socketPath(), "stop")
	for deadline := time.Now().Add(startingWait); errors.Is(err, os.ErrDeadlineExceeded) && time.Now().Before(deadline); {
		resp, err = callControl(inst.socketPath(), "stop")
	}
	switch {
	case errors.Is(err, errNotRunning):
		_, _ = b.tool.output("schtasks", "/End", "/TN", name)
		return nil
	case err != nil:
		_, _ = b.tool.output("schtasks", "/End", "/TN", name)
		return b.waitExit(procalive.Identity{})
	}
	daemon := resp.identity()
	if b.waitExit(daemon) == nil {
		return nil
	}
	_, _ = b.tool.output("schtasks", "/End", "/TN", name)
	return b.waitExit(daemon)
}

func (t taskScheduler) restart(b *Battery) error {
	if err := t.stop(b); err != nil {
		return err
	}
	return t.start(b)
}

func (t taskScheduler) installed(b *Battery) (bool, error) {
	name, err := t.taskName(b)
	if err != nil {
		return false, err
	}
	out, err := b.tool.output("schtasks", "/Query", "/TN", name)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, toolError("schtasks", []string{"/Query", "/TN", name}, out, err)
}

func (taskScheduler) logs(b *Battery, n int, follow bool) error {
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	return tailFile(b.cfg.Stdout, inst.logPath(), n, follow)
}
