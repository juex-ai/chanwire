package e2e

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func RunCLI(t *testing.T, bin, endpoint, dir string, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = Env(endpoint, dir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("chanwire %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	if stderr.Len() > 0 {
		t.Logf("chanwire %s stderr:\n%s", strings.Join(args, " "), stderr.String())
	}
	return stdout.String()
}

type CLIConnect struct {
	cancel context.CancelFunc
	lines  chan string
	done   chan error
	stderr SafeBuffer
}

func StartCLIConnect(t *testing.T, bin, endpoint, dir string) *CLIConnect {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "connect")
	cmd.Env = Env(endpoint, dir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("connect stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		t.Fatalf("connect stderr pipe: %v", err)
	}

	c := &CLIConnect{
		cancel: cancel,
		lines:  make(chan string, 32),
		done:   make(chan error, 1),
	}

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start chanwire connect: %v", err)
	}

	go ScanLines(stdout, c.lines)
	go func() {
		raw, _ := io.ReadAll(stderr)
		c.stderr.Write(string(raw))
	}()
	go func() {
		c.done <- cmd.Wait()
		close(c.done)
	}()

	return c
}

func (c *CLIConnect) WaitForLine(t *testing.T, contains string) string {
	t.Helper()

	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				t.Fatalf("chanwire connect exited before line containing %q\nstderr:\n%s", contains, c.stderr.String())
			}
			if strings.Contains(line, contains) {
				return line
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for connect line containing %q\nstderr:\n%s", contains, c.stderr.String())
		}
	}
}

// AssertNoFurtherLineContaining is only for terminal checks where no later
// stdout lines are expected before the next test action. It consumes lines
// while watching for the forbidden substring.
func (c *CLIConnect) AssertNoFurtherLineContaining(t *testing.T, contains string, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				return
			}
			if strings.Contains(line, contains) {
				t.Fatalf("unexpected connect line containing %q: %s\nstderr:\n%s", contains, line, c.stderr.String())
			}
		case <-timer.C:
			return
		}
	}
}

func (c *CLIConnect) Stop() {
	c.cancel()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
	}
}

func ScanLines(r io.Reader, out chan<- string) {
	defer close(out)

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				idx := bytes.IndexByte(buf, '\n')
				if idx < 0 {
					break
				}
				out <- string(buf[:idx])
				buf = append(buf[:0], buf[idx+1:]...)
			}
		}
		if err != nil {
			if len(buf) > 0 {
				out <- string(buf)
			}
			return
		}
	}
}
