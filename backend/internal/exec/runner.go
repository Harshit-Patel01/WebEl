package exec

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

const ringBufferSize = 500

// maxLogLine bounds a single captured log line. A bare bufio.Scanner defaults
// to 64KB and silently truncates anything larger, which loses build output.
// ponytail: 1MB per line is generous; raise only if real builds exceed it.
const maxLogLine = 1024 * 1024

type LogLevel string

const (
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
	LevelOK    LogLevel = "OK"
)

type LogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"` // "stdout" or "stderr"
	Text      string    `json:"text"`
	Level     LogLevel  `json:"level"`
}

type ExecResult struct {
	JobID     string        `json:"job_id"`
	Command   string        `json:"command"`
	Args      []string      `json:"args"`
	ExitCode  int           `json:"exit_code"`
	Success   bool          `json:"success"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
	Duration  time.Duration `json:"duration"`
	Lines     []LogLine     `json:"lines"`
	Error     string        `json:"error,omitempty"`
}

type RunOpts struct {
	JobID          string
	JobType        string
	Command        string
	Args           []string
	WorkDir        string
	Env            map[string]string
	Timeout        time.Duration
	MergeEnv       bool   // merge with system env
	BroadcastJobID string // if set, broadcast log_line events to this topic instead of JobID
}

type Broadcaster interface {
	BroadcastToJob(jobID string, msg interface{})
}

type Runner struct {
	broadcaster Broadcaster
	db          *state.DB
	logger      *zap.Logger
	logDir      string

	mu         sync.Mutex
	activeJobs map[string]context.CancelFunc
	buffers    map[string]*RingBuffer
	pgids      map[string]int
	cancelled  map[string]bool
}

type RingBuffer struct {
	mu    sync.Mutex
	lines []LogLine
	size  int
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		lines: make([]LogLine, 0, size),
		size:  size,
	}
}

func (rb *RingBuffer) Add(line LogLine) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.lines) >= rb.size {
		rb.lines = rb.lines[1:]
	}
	rb.lines = append(rb.lines, line)
}

func (rb *RingBuffer) GetAll() []LogLine {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]LogLine, len(rb.lines))
	copy(out, rb.lines)
	return out
}

func NewRunner(broadcaster Broadcaster, db *state.DB, logger *zap.Logger, logDir string) *Runner {
	return &Runner{
		broadcaster: broadcaster,
		db:          db,
		logger:      logger,
		logDir:      logDir,
		activeJobs:  make(map[string]context.CancelFunc),
		buffers:     make(map[string]*RingBuffer),
		pgids:       make(map[string]int),
		cancelled:   make(map[string]bool),
	}
}

// newLogScanner tolerates lines up to maxLogLine. A bare bufio.Scanner stops at
// its 64KB default and drops the rest silently.
func newLogScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), maxLogLine)
	return s
}

func (r *Runner) RunWithStdin(ctx context.Context, opts RunOpts, stdin io.Reader) (*ExecResult, error) {
	if opts.JobID == "" {
		opts.JobID = uuid.New().String()
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)

	r.mu.Lock()
	r.activeJobs[opts.JobID] = cancel
	r.buffers[opts.JobID] = NewRingBuffer(ringBufferSize)
	r.mu.Unlock()

	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.activeJobs, opts.JobID)
		delete(r.buffers, opts.JobID)
		delete(r.pgids, opts.JobID)
		delete(r.cancelled, opts.JobID)
		r.mu.Unlock()
	}()

	job := &state.Job{
		ID:      opts.JobID,
		Type:    opts.JobType,
		Status:  "running",
		Command: fmt.Sprintf("%s %s", opts.Command, joinArgs(opts.Args)),
	}
	if err := r.db.CreateJob(job); err != nil {
		r.logger.Error("failed to create job record", zap.Error(err))
	}

	// Determine which topic to broadcast to
	broadcastID := opts.JobID
	if opts.BroadcastJobID != "" {
		broadcastID = opts.BroadcastJobID
	}

	r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
		"type":    "job_started",
		"jobId":   broadcastID,
		"command": job.Command,
	})

	result := &ExecResult{
		JobID:     opts.JobID,
		Command:   opts.Command,
		Args:      opts.Args,
		StartedAt: time.Now(),
	}

	cmd := osexec.CommandContext(ctx, opts.Command, opts.Args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	if opts.MergeEnv {
		cmd.Env = os.Environ()
	}
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// ---- this is the only difference from Run ----
	cmd.Stdin = stdin
	// -----------------------------------------------

	setPgid(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	var logFile *os.File
	logPath := filepath.Join(r.logDir, opts.JobID+".log")
	logFile, err = os.Create(logPath)
	if err != nil {
		r.logger.Warn("failed to create log file", zap.Error(err))
	}

	if err := cmd.Start(); err != nil {
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = result.EndedAt.Sub(result.StartedAt)
		r.finalizeJob(job, result, logPath, broadcastID)
		return result, nil
	}

	// setPgid makes the child its own process-group leader, so its PID is also
	// its PGID and Cancel can take down the whole tree.
	r.mu.Lock()
	r.pgids[opts.JobID] = cmd.Process.Pid
	r.mu.Unlock()

	r.logger.Info("job started",
		zap.String("jobId", opts.JobID),
		zap.String("command", opts.Command),
	)

	var wg sync.WaitGroup
	var linesMu sync.Mutex

	readStream := func(scanner *bufio.Scanner, stream string) {
		defer wg.Done()
		for scanner.Scan() {
			text := scanner.Text()
			cleaned := StripANSI(text)
			level := DetectLevel(cleaned)

			line := LogLine{
				Timestamp: time.Now(),
				Stream:    stream,
				Text:      cleaned,
				Level:     level,
			}

			r.mu.Lock()
			if buf, ok := r.buffers[opts.JobID]; ok {
				buf.Add(line)
			}
			r.mu.Unlock()

			// stdout and stderr are read concurrently; guard the shared slice.
			linesMu.Lock()
			result.Lines = append(result.Lines, line)
			linesMu.Unlock()

			if logFile != nil {
				fmt.Fprintf(logFile, "[%s] [%s] [%s] %s\n",
					line.Timestamp.Format(time.RFC3339),
					stream, level, cleaned)
			}

			r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
				"type":  "log_line",
				"jobId": broadcastID,
				"line":  line,
			})

			if pct, phase, ok := DetectProgress(cleaned); ok {
				r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
					"type":    "progress",
					"jobId":   broadcastID,
					"percent": pct,
					"phase":   phase,
				})
			}
		}
		if err := scanner.Err(); err != nil {
			r.logger.Warn("log stream read ended early",
				zap.String("jobId", opts.JobID),
				zap.String("stream", stream),
				zap.Error(err))
		}
	}

	wg.Add(2)
	go readStream(newLogScanner(stdout), "stdout")
	go readStream(newLogScanner(stderr), "stderr")

	wg.Wait()

	err = cmd.Wait()
	result.EndedAt = time.Now()
	result.Duration = result.EndedAt.Sub(result.StartedAt)

	if err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
	} else {
		result.ExitCode = 0
		result.Success = true
	}

	if logFile != nil {
		logFile.Close()
	}

	r.finalizeJob(job, result, logPath, broadcastID)

	return result, nil
}

func (r *Runner) Run(ctx context.Context, opts RunOpts) (*ExecResult, error) {
	if opts.JobID == "" {
		opts.JobID = uuid.New().String()
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Minute
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)

	// Track active job
	r.mu.Lock()
	r.activeJobs[opts.JobID] = cancel
	r.buffers[opts.JobID] = NewRingBuffer(ringBufferSize)
	r.mu.Unlock()

	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.activeJobs, opts.JobID)
		delete(r.buffers, opts.JobID)
		delete(r.pgids, opts.JobID)
		delete(r.cancelled, opts.JobID)
		r.mu.Unlock()
	}()

	// Persist job to DB
	job := &state.Job{
		ID:      opts.JobID,
		Type:    opts.JobType,
		Status:  "running",
		Command: fmt.Sprintf("%s %s", opts.Command, joinArgs(opts.Args)),
	}
	if err := r.db.CreateJob(job); err != nil {
		r.logger.Error("failed to create job record", zap.Error(err))
	}

	// Determine which topic to broadcast to
	broadcastID := opts.JobID
	if opts.BroadcastJobID != "" {
		broadcastID = opts.BroadcastJobID
	}

	// Broadcast job started
	r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
		"type":    "job_started",
		"jobId":   broadcastID,
		"command": job.Command,
	})

	result := &ExecResult{
		JobID:     opts.JobID,
		Command:   opts.Command,
		Args:      opts.Args,
		StartedAt: time.Now(),
	}

	// Build command
	cmd := osexec.CommandContext(ctx, opts.Command, opts.Args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	// Set up environment
	if opts.MergeEnv {
		cmd.Env = os.Environ()
	}
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set process group so we can kill the entire tree
	setPgid(cmd)

	// Record the PGID once started; see the identical block in RunWithStdin.

	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	// Open log file
	var logFile *os.File
	logPath := filepath.Join(r.logDir, opts.JobID+".log")
	logFile, err = os.Create(logPath)
	if err != nil {
		r.logger.Warn("failed to create log file", zap.Error(err))
	}

	// Start command
	if err := cmd.Start(); err != nil {
		result.Error = err.Error()
		result.EndedAt = time.Now()
		result.Duration = result.EndedAt.Sub(result.StartedAt)
		r.finalizeJob(job, result, logPath, broadcastID)
		return result, nil
	}

	// setPgid makes the child its own process-group leader, so its PID is also
	// its PGID and Cancel can take down the whole tree.
	r.mu.Lock()
	r.pgids[opts.JobID] = cmd.Process.Pid
	r.mu.Unlock()

	r.logger.Info("job started",
		zap.String("jobId", opts.JobID),
		zap.String("command", opts.Command),
	)

	// Read stdout and stderr concurrently
	var wg sync.WaitGroup
	var linesMu sync.Mutex

	readStream := func(scanner *bufio.Scanner, stream string) {
		defer wg.Done()
		for scanner.Scan() {
			text := scanner.Text()
			cleaned := StripANSI(text)
			level := DetectLevel(cleaned)

			line := LogLine{
				Timestamp: time.Now(),
				Stream:    stream,
				Text:      cleaned,
				Level:     level,
			}

			// Store in ring buffer
			r.mu.Lock()
			if buf, ok := r.buffers[opts.JobID]; ok {
				buf.Add(line)
			}
			r.mu.Unlock()

			// Append to result
			linesMu.Lock()
			result.Lines = append(result.Lines, line)
			linesMu.Unlock()

			// Write to log file
			if logFile != nil {
				fmt.Fprintf(logFile, "[%s] [%s] [%s] %s\n",
					line.Timestamp.Format(time.RFC3339),
					stream, level, cleaned)
			}

			// Broadcast to WebSocket clients (use broadcastID for correct topic routing)
			r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
				"type":  "log_line",
				"jobId": broadcastID,
				"line":  line,
			})

			// Check for progress
			if pct, phase, ok := DetectProgress(cleaned); ok {
				r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
					"type":    "progress",
					"jobId":   broadcastID,
					"percent": pct,
					"phase":   phase,
				})
			}
		}
		if err := scanner.Err(); err != nil {
			r.logger.Warn("log stream read ended early",
				zap.String("jobId", opts.JobID),
				zap.String("stream", stream),
				zap.Error(err))
		}
	}

	wg.Add(2)
	go readStream(newLogScanner(stdout), "stdout")
	go readStream(newLogScanner(stderr), "stderr")

	wg.Wait()

	// Wait for command to finish
	err = cmd.Wait()
	result.EndedAt = time.Now()
	result.Duration = result.EndedAt.Sub(result.StartedAt)

	if err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
	} else {
		result.ExitCode = 0
		result.Success = true
	}

	if logFile != nil {
		logFile.Close()
	}

	r.finalizeJob(job, result, logPath, broadcastID)

	return result, nil
}

func (r *Runner) Cancel(jobID string) error {
	r.mu.Lock()
	cancel, ok := r.activeJobs[jobID]
	pgid := r.pgids[jobID]
	if ok {
		// Recorded before signalling so finalizeJob keeps this status instead of
		// downgrading the job to "failed".
		r.cancelled[jobID] = true
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("job %s not found or already completed", jobID)
	}

	cancel()
	// Cancel the context signal only reaches the direct child; its descendants
	// (npm, lxc, pip) would keep running and keep burning CPU on the device.
	if err := killTree(pgid); err != nil {
		r.logger.Warn("failed to signal job process group",
			zap.String("jobId", jobID),
			zap.Int("pgid", pgid),
			zap.Error(err))
	}

	// Update job status
	now := time.Now()
	j := &state.Job{
		ID:       jobID,
		Status:   "cancelled",
		EndedAt:  &now,
		ExitCode: -1,
	}
	r.db.UpdateJob(j)

	r.broadcaster.BroadcastToJob(jobID, map[string]interface{}{
		"type":  "job_failed",
		"jobId": jobID,
		"error": "cancelled by user",
	})

	return nil
}

func (r *Runner) GetJobBuffer(jobID string) []LogLine {
	r.mu.Lock()
	buf, ok := r.buffers[jobID]
	r.mu.Unlock()
	if !ok {
		return nil
	}
	return buf.GetAll()
}

func (r *Runner) IsJobRunning(jobID string) bool {
	r.mu.Lock()
	_, ok := r.activeJobs[jobID]
	r.mu.Unlock()
	return ok
}

func (r *Runner) finalizeJob(job *state.Job, result *ExecResult, logPath string, broadcastID string) {
	now := time.Now()

	r.mu.Lock()
	wasCancelled := r.cancelled[result.JobID]
	r.mu.Unlock()

	switch {
	case wasCancelled:
		// Cancel already wrote the authoritative status; don't overwrite it.
		job.Status = "cancelled"
	case result.Success:
		job.Status = "complete"
	default:
		job.Status = "failed"
	}
	job.EndedAt = &now
	job.ExitCode = result.ExitCode
	job.LogPath = logPath

	if err := r.db.UpdateJob(job); err != nil {
		r.logger.Error("failed to update job record", zap.Error(err))
	}

	// Broadcast completion to the correct topic
	if broadcastID == "" {
		broadcastID = result.JobID
	}
	if result.Success {
		r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
			"type":     "job_complete",
			"jobId":    broadcastID,
			"exitCode": result.ExitCode,
			"duration": result.Duration.String(),
		})
	} else if wasCancelled {
		// Cancel already broadcast the reason; a second event only confuses the UI.
		job.Status = "cancelled"
	} else {
		r.broadcaster.BroadcastToJob(broadcastID, map[string]interface{}{
			"type":  "job_failed",
			"jobId": broadcastID,
			"error": result.Error,
		})
	}

	r.logger.Info("job completed",
		zap.String("jobId", result.JobID),
		zap.Bool("success", result.Success),
		zap.Int("exitCode", result.ExitCode),
		zap.Duration("duration", result.Duration),
	)
}

func joinArgs(args []string) string {
	s := ""
	for i, a := range args {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}
