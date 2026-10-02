// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// commandTimeout bounds a command that is expected to run to completion.
	commandTimeout = 60 * time.Second
	// readyTimeout bounds the wait for a started server to answer its readiness route.
	readyTimeout = 15 * time.Second
	// pollInterval is the pause between two polls of a condition.
	pollInterval = 50 * time.Millisecond
	// startAttempts is how many times a server start is tried, each with a new port.
	startAttempts = 2

	// readyPath is the readiness route of the ibdm server.
	readyPath = "/-/ready"
	// logLevelArg makes every run log at debug level, so failures are easy to diagnose.
	logLevelArg = "--log-level=debug"

	// dataRaceMarker opens every report of the race detector. The Go runtime turns a race into
	// exit code 66 only when the process would exit 0, so the report is the only reliable signal.
	dataRaceMarker = "WARNING: DATA RACE"

	logMessageKey = "@message"
	logLevelKey   = "@level"
)

// result is the outcome of a command run to completion.
type result struct {
	exitCode int
	stdout   string
	stderr   string
	// logs holds the stderr lines that are JSON log records.
	logs []map[string]any
}

// hasLog reports whether a log record has exactly the given level and message.
func (r result) hasLog(level, message string) bool {
	return countLogs(r.logs, level, message) > 0
}

// countLogs counts the log records with exactly the given level and message. It matches the
// message field, never a substring of the whole line, because error texts repeat messages.
func countLogs(logs []map[string]any, level, message string) int {
	count := 0
	for _, record := range logs {
		if record[logLevelKey] == level && record[logMessageKey] == message {
			count++
		}
	}
	return count
}

// parseLogs decodes the stderr lines that are JSON objects. Other lines, such as the error a
// failed command prints, are left to the plain stderr text.
func parseLogs(stderr string) []map[string]any {
	var logs []map[string]any
	for line := range strings.SplitSeq(stderr, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err == nil {
			logs = append(logs, record)
		}
	}
	return logs
}

// hasDataRace reports whether stderr holds a report of the race detector.
func hasDataRace(stderr string) bool {
	return strings.Contains(stderr, dataRaceMarker)
}

// assertNoDataRace fails the test when the stderr of an ibdm process holds a race report,
// whatever its exit code, and prints the whole stderr. It never stops the test, so it is safe
// in a cleanup.
func assertNoDataRace(t *testing.T, stderr string) {
	t.Helper()

	if hasDataRace(stderr) {
		t.Errorf("the race detector reported a data race in ibdm; stderr:\n%s", stderr)
	}
}

// commandEnv builds the environment of the binary from env alone. The environment of the test
// process is never inherited, so no credential of the developer reaches the binary.
func commandEnv(t *testing.T, env map[string]string) []string {
	t.Helper()

	commandEnv := make([]string, 0, len(env)+1)
	commandEnv = append(commandEnv, "HOME="+t.TempDir())
	for key, value := range env {
		commandEnv = append(commandEnv, key+"="+value)
	}
	return commandEnv
}

// newCommand prepares binary with args, the debug log level and the environment env.
func newCommand(ctx context.Context, t *testing.T, binary string, env map[string]string, args ...string) *exec.Cmd {
	t.Helper()

	cmd := exec.CommandContext(ctx, binary, append(args, logLevelArg)...)
	cmd.Env = commandEnv(t, env)
	return cmd
}

// runIBDM runs the binary built by TestMain to completion with args and the environment env,
// and fails the test if it does not end within commandTimeout.
func runIBDM(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()

	return runBinary(t, ibdmBinary, env, args...)
}

// runBinary runs binary to completion as runIBDM does. It lets a test run another ibdm binary,
// such as a previous release.
func runBinary(t *testing.T, binary string, env map[string]string, args ...string) result {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), commandTimeout)
	defer cancel()

	cmd := newCommand(ctx, t, binary, env, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, ctx.Err(), "ibdm %v did not end within %s; stderr:\n%s", args, commandTimeout, stderr.String())
	assertNoDataRace(t, stderr.String())

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "running ibdm %v", args)
		exitCode = exitErr.ExitCode()
	}

	return result{
		exitCode: exitCode,
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		logs:     parseLogs(stderr.String()),
	}
}

// syncBuffer is a bytes.Buffer safe for a process writing to it while a test reads it.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// Write appends p to the buffer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

// String returns the content written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// process is a server started with startIBDM. The test cleanup kills it.
type process struct {
	baseURL string
	stderr  *syncBuffer
	done    chan struct{}
}

// logs returns the JSON log records written so far.
func (p *process) logs() []map[string]any {
	return parseLogs(p.stderr.String())
}

// startIBDM starts the binary as a server with args and the environment env, on 127.0.0.1 and a
// free port, and returns once the readiness route answers. ibdm in webhook mode never returns
// on its own, so the test cleanup kills the process. A start that fails is retried once with a
// new port, since the free port can be taken between its discovery and its use.
func startIBDM(t *testing.T, env map[string]string, args ...string) *process {
	t.Helper()

	var lastStderr string
	for range startAttempts {
		proc, stderr := tryStart(t, env, args...)
		if proc != nil {
			return proc
		}
		lastStderr = stderr
	}

	require.FailNow(t, "ibdm did not become ready", "ibdm %v; stderr of the last attempt:\n%s", args, lastStderr)
	return nil
}

// tryStart makes one start attempt. It returns the process once ready, or nil and the stderr of
// a process that exited or did not become ready in time, after stopping it.
func tryStart(t *testing.T, env map[string]string, args ...string) (*process, string) {
	t.Helper()

	port := freePort(t)
	serverEnv := map[string]string{"HTTP_HOST": "127.0.0.1", "HTTP_PORT": strconv.Itoa(port)}
	for key, value := range env {
		serverEnv[key] = value
	}

	ctx, cancel := context.WithCancel(t.Context())
	cmd := newCommand(ctx, t, ibdmBinary, serverEnv, args...)
	proc := &process{
		baseURL: "http://127.0.0.1:" + strconv.Itoa(port),
		stderr:  new(syncBuffer),
		done:    make(chan struct{}),
	}
	cmd.Stdout = new(syncBuffer)
	cmd.Stderr = proc.stderr
	require.NoError(t, cmd.Start())

	go func() {
		// The exit status of a killed server carries no information.
		_ = cmd.Wait()
		close(proc.done)
	}()
	stop := func() {
		cancel()
		<-proc.done
	}

	if !waitReady(t, proc) {
		stop()
		assertNoDataRace(t, proc.stderr.String())
		return nil, proc.stderr.String()
	}

	// The process is killed at cleanup; a race report it wrote while running is still in its
	// stderr, and must fail the test.
	t.Cleanup(func() {
		stop()
		assertNoDataRace(t, proc.stderr.String())
	})
	return proc, ""
}

// waitReady polls the readiness route until it answers 200, the process exits, or readyTimeout
// passes.
func waitReady(t *testing.T, proc *process) bool {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-proc.done:
			return false
		default:
		}

		if isReady(t, client, proc.baseURL+readyPath) {
			return true
		}
		time.Sleep(pollInterval)
	}
	return false
}

// isReady reports whether url answers 200.
func isReady(t *testing.T, client *http.Client, url string) bool {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// freePort returns a TCP port free on 127.0.0.1 at the time of the call.
func freePort(t *testing.T) int {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	return addr.Port
}
