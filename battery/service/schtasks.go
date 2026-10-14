package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"unicode/utf16"
)

// taskScheduler is the Windows backend: a Task Scheduler task with a
// logon trigger for the current user, running at least privilege in
// the user's own session. A Windows service would run in session 0
// (no UI, no mapped drives, a different account) and needs an admin to
// install, so it is not the per-user mechanism.
//
// Task Scheduler sends no stop signal a console-less process can
// catch, so `stop` asks the daemon to drain over the control channel
// and only falls back to `schtasks /End` (a hard kill) when nothing
// answers.
type taskScheduler struct{}

func (taskScheduler) kind() string { return "Task Scheduler" }

// taskXML renders the task definition. schtasks /XML expects UTF-16
// with a byte-order mark; the caller encodes.
func (taskScheduler) taskXML(b *Battery, exe string, port int, account string) string {
	esc := func(s string) string {
		var buf bytes.Buffer
		_ = xml.EscapeText(&buf, []byte(s))
		return buf.String()
	}
	args := daemonArgs(exe, port)[1:]
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
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
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

// encodeUTF16 returns s as UTF-16LE with a byte-order mark.
func encodeUTF16(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(u))
	out[0], out[1] = 0xFF, 0xFE
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// quoteWindowsArg quotes one argument for the CommandLineToArgvW
// rules: backslashes are literal unless they precede a double quote.
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
			// The run's backslashes are already written: double them
			// and escape the quote.
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

func (t taskScheduler) install(b *Battery, exe string, port int) error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("service: resolve current user: %w", err)
	}
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	if err := inst.ensure(); err != nil {
		return err
	}
	f, err := os.CreateTemp(inst.dir, "task-*.xml")
	if err != nil {
		return fmt.Errorf("service: write task definition: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(encodeUTF16(t.taskXML(b, exe, port, u.Username))); err != nil {
		f.Close()
		return fmt.Errorf("service: write task definition: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("service: write task definition: %w", err)
	}
	// A running instance holds the old binary; stop it so /Run starts
	// the new one (MultipleInstancesPolicy IgnoreNew would skip it).
	_ = t.stop(b)
	if err := b.run("schtasks", "/Create", "/TN", b.cfg.ID, "/XML", f.Name(), "/F"); err != nil {
		return err
	}
	return b.run("schtasks", "/Run", "/TN", b.cfg.ID)
}

func (t taskScheduler) uninstall(b *Battery) error {
	_ = t.stop(b)
	ok, err := t.installed(b)
	if err != nil || !ok {
		return err
	}
	return b.run("schtasks", "/Delete", "/TN", b.cfg.ID, "/F")
}

func (t taskScheduler) start(b *Battery) error {
	return b.run("schtasks", "/Run", "/TN", b.cfg.ID)
}

func (t taskScheduler) stop(b *Battery) error {
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	if _, err := callControl(inst.socketPath(), "stop"); err == nil {
		return nil
	} else if !errors.Is(err, errNotRunning) {
		return err
	}
	// Nothing answered: end a task stuck before its control channel
	// opened. "Not running" is not an error.
	_, _ = b.tool.output("schtasks", "/End", "/TN", b.cfg.ID)
	return nil
}

func (t taskScheduler) restart(b *Battery) error {
	if err := t.stop(b); err != nil {
		return err
	}
	return t.start(b)
}

func (t taskScheduler) installed(b *Battery) (bool, error) {
	out, err := b.tool.output("schtasks", "/Query", "/TN", b.cfg.ID)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, toolError("schtasks", []string{"/Query", "/TN", b.cfg.ID}, out, err)
}

func (taskScheduler) logs(b *Battery, n int, follow bool) error {
	inst, err := b.layoutFor(false)
	if err != nil {
		return err
	}
	return tailFile(b.cfg.Stdout, inst.logPath(), n, follow)
}
