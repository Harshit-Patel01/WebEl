package services

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/opendeploy/opendeploy/internal/exec"
)

func repeatLines(n int, text string) []exec.LogLine {
	lines := make([]exec.LogLine, n)
	for i := range lines {
		lines[i] = exec.LogLine{Stream: "stdout", Text: text}
	}
	return lines
}

func TestCommandFailure(t *testing.T) {
	tests := []struct {
		name    string
		result  *exec.ExecResult
		err     error
		want    []string
		notWant string
	}{
		{
			name:   "nil result and nil err reports missing result",
			result: nil,
			err:    nil,
			want:   []string{"no result returned"},
		},
		{
			name:   "runner error is wrapped",
			result: &exec.ExecResult{ExitCode: -1},
			err:    errors.New("boom"),
			want:   []string{"boom"},
		},
		{
			name: "non-zero exit with no output names the action",
			// The runner returns a nil error for non-zero exits, so this is the
			// case that used to render as a bare "%!w(<nil>)".
			result: &exec.ExecResult{ExitCode: 1},
			want:   []string{"init container", "exit code 1"},
			// The old formatting bug must not come back.
			notWant: "%!w(<nil>)",
		},
		{
			name: "command output is included",
			result: &exec.ExecResult{
				ExitCode: 1,
				Lines: []exec.LogLine{
					{Stream: "stderr", Text: "Error: image not found"},
				},
			},
			want: []string{"exit code 1", "Error: image not found"},
		},
		{
			name: "output is truncated to 20 lines",
			result: &exec.ExecResult{
				ExitCode: 1,
				Lines:    repeatLines(50, "apk: fetch failed"),
			},
			want: []string{"and 30 more lines"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandFailure("failed to init container", tt.result, tt.err).Error()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("error %q does not contain %q", got, w)
				}
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Errorf("error %q should not contain %q", got, tt.notWant)
			}
		})
	}

	// A zero exit code with a nil error means the runner reported failure
	// without an exit status; it must not be reported as success.
	if err := commandFailure("failed to init container", &exec.ExecResult{}, nil); err == nil {
		t.Error("expected an error, got nil")
	} else if strings.Contains(err.Error(), "exit code 0") {
		t.Errorf("zero exit code should be replaced by a sentinel: %v", fmt.Sprint(err))
	}
}
