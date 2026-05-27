package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNormalizeFormatDefaultsToPretty(t *testing.T) {
	if got := normalizeFormat(""); got != "pretty" {
		t.Fatalf("normalizeFormat(empty) = %q", got)
	}
}

func TestNormalizeFormatAcceptsJSON(t *testing.T) {
	if got := normalizeFormat("json"); got != "json" {
		t.Fatalf("normalizeFormat(json) = %q", got)
	}
}

func TestNormalizeFormatFallsBackToPretty(t *testing.T) {
	if got := normalizeFormat("garbage"); got != "pretty" {
		t.Fatalf("normalizeFormat(garbage) = %q", got)
	}
}

func TestNewLoggerUsesPrettyOutputByDefault(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "dashboard", "")
	logger.Info().Msg("hello")

	output := buf.String()
	if !strings.Contains(output, "hello") {
		t.Fatalf("pretty output missing message: %q", output)
	}
	if strings.Contains(output, `"level"`) {
		t.Fatalf("expected pretty output, got json-like output: %q", output)
	}
}

func TestNewLoggerUsesJSONWhenRequested(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "dashboard", "json")
	logger.Info().Msg("hello")

	output := buf.String()
	if !strings.Contains(output, `"level":"info"`) {
		t.Fatalf("expected json output, got %q", output)
	}
	if !strings.Contains(output, `"component":"dashboard"`) {
		t.Fatalf("json output missing component: %q", output)
	}
}
