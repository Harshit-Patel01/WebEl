package services

import (
	"errors"
	"testing"

	"github.com/opendeploy/opendeploy/internal/exec"
)

// execDetail exists because the original code did
// fmt.Errorf("failed to init container: %w", initErr) while the real failure
// lived in initResult. When initErr was nil that printed the literal
// "%!w(<nil>)" and hid the actual cause.
func TestExecDetailPrefersResultOverNilErr(t *testing.T) {
	res := &exec.ExecResult{
		Success: false,
		Error:   "Failed initialising instance: No root device could be found",
	}

	// The regression: err is nil, which used to render as "%!w(<nil>)".
	got := execDetail(res, nil)
	if got != res.Error {
		t.Fatalf("want the runner's error, got %q", got)
	}
	if got == "" || got == "<nil>" {
		t.Fatal("detail must never be empty for a failed run")
	}
}

func TestExecDetailFallsBackThroughSources(t *testing.T) {
	// No Error field, but stderr output is present.
	withLines := &exec.ExecResult{
		Lines: []exec.LogLine{
			{Stream: "stdout", Text: "Creating"},
			{Stream: "stderr", Text: "No root device could be found"},
		},
	}
	if got := execDetail(withLines, nil); got != "No root device could be found" {
		t.Fatalf("want last log line, got %q", got)
	}

	// Only a transport error available.
	if got := execDetail(nil, errors.New("exec: not found")); got != "exec: not found" {
		t.Fatalf("want the transport error, got %q", got)
	}

	// Nothing at all must still yield something printable.
	if got := execDetail(nil, nil); got == "" {
		t.Fatal("must never return an empty string")
	}
}
