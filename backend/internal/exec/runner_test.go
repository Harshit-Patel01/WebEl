package exec

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

type stubBroadcaster struct{}

func (stubBroadcaster) BroadcastToJob(string, interface{}) {}

func newTestRunner(t *testing.T) (*Runner, *state.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := state.NewDB(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRunner(stubBroadcaster{}, db, zap.NewNop(), dir), db
}

// A finished job must not leave its ring buffer behind. buffers was only ever
// populated, never deleted, so every command ever run leaked one.
func TestFinishedJobReleasesState(t *testing.T) {
	r, _ := newTestRunner(t)

	res, err := r.Run(context.Background(), RunOpts{
		JobType: "test",
		Command: "/bin/echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got exit %d", res.ExitCode)
	}

	r.mu.Lock()
	_, leakedBuffer := r.buffers[res.JobID]
	_, leakedActive := r.activeJobs[res.JobID]
	_, leakedCANCEL := r.cancelled[res.JobID]
	_, leakedPgid := r.pgids[res.JobID]
	r.mu.Unlock()

	if leakedBuffer {
		t.Error("buffers retained after completion (leak)")
	}
	if leakedActive {
		t.Error("activeJobs retained after completion")
	}
	if leakedCANCEL {
		t.Error("cancelled retained after completion")
	}
	if leakedPgid {
		t.Error("pgids retained after completion")
	}
}

// stdout and stderr are drained by two goroutines; unsynchronised appends to the
// shared slice are a data race. Run under -race.
func TestConcurrentStreamCaptureIsRaceFree(t *testing.T) {
	r, _ := newTestRunner(t)

	res, err := r.Run(context.Background(), RunOpts{
		JobType: "test",
		Command: "/bin/sh",
		Args:    []string{"-c", "i=0; while [ $i -lt 200 ]; do echo out-$i; echo err-$i >&2; i=$((i+1)); done"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var out, errOut int
	for _, l := range res.Lines {
		switch l.Stream {
		case "stdout":
			out++
		case "stderr":
			errOut++
		}
	}
	if out != 200 || errOut != 200 {
		t.Fatalf("lost lines: stdout=%d stderr=%d, want 200 each", out, errOut)
	}
}

// bufio.Scanner stops at its 64KB default and drops the rest with no error.
// npm and minified output regularly exceed that.
func TestLongLineIsCapturedWhole(t *testing.T) {
	r, _ := newTestRunner(t)

	const size = 200 * 1024
	res, err := r.Run(context.Background(), RunOpts{
		JobType: "test",
		Command: "/bin/sh",
		Args:    []string{"-c", fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'x'", size)},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got int
	for _, l := range res.Lines {
		got += len(l.Text)
	}
	if got < size {
		t.Errorf("truncated long line: captured %d bytes, want %d", got, size)
	}
}

// Cancel records the intent so finalizeJob cannot downgrade the job to "failed",
// and it must survive a job that exits non-zero as a result of the kill.
func TestCancelStatusSurvivesFinalize(t *testing.T) {
	r, db := newTestRunner(t)

	// Long-running job we cancel mid-flight.
	jobID := "cancel-status-test"
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.Run(context.Background(), RunOpts{
			JobID:   jobID,
			JobType: "test",
			Command: "/bin/sh",
			Args:    []string{"-c", "sleep 30"},
		})
	}()

	// Wait until the runner reports it active, then cancel.
	for i := 0; i < 200 && !r.IsJobRunning(jobID); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !r.IsJobRunning(jobID) {
		t.Fatal("job never became active")
	}

	if err := r.Cancel(jobID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	<-done

	job, err := db.GetJob(jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.Status != "cancelled" {
		t.Errorf("status = %q, want \"cancelled\"", job.Status)
	}
}

// A cancelled job's descendants must die too. The shell spawns a nested sleep;
// if only the direct child were signalled, the grandchild would outlive it.
func TestCancelKillsProcessTree(t *testing.T) {
	r, _ := newTestRunner(t)

	jobID := "kill-tree-test"
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.Run(context.Background(), RunOpts{
			JobID:   jobID,
			JobType: "test",
			Command: "/bin/sh",
			Args:    []string{"-c", "sleep 30 & echo $!; wait"},
		})
	}()

	for i := 0; i < 200 && !r.IsJobRunning(jobID); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if err := r.Cancel(jobID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("job did not exit within 10s of Cancel")
	}

	r.mu.Lock()
	_, stillRunning := r.activeJobs[jobID]
	r.mu.Unlock()
	if stillRunning {
		t.Error("job still registered as active after Cancel")
	}
}
