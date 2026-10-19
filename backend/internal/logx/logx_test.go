package logx

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestErrorIncludesOperationErrorAndStack(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	Error("save profile", errors.New("database unavailable"))
	got := output.String()
	for _, want := range []string{"ERROR save profile: database unavailable", "stacktrace:", "internal/logx.Error"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log does not contain %q:\n%s", want, got)
		}
	}
}

func TestErrorIgnoresNil(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	Error("ignored", nil)
	if output.Len() != 0 {
		t.Fatalf("nil error was logged: %s", output.String())
	}
}
