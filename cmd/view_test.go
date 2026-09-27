package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestViewCommandHelpAndUsage(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"view", "--help"}, nil, &out, &stderr); code != 0 || !strings.Contains(out.String(), "--max-documents") {
		t.Fatal(code, out.String(), stderr.String())
	}
	for _, flags := range [][]string{{"--json"}, {"--timeout", "0s"}, {"--max-documents", "0"}, {"--addr", "0.0.0.0:5484"}, {"extra"}} {
		out.Reset()
		stderr.Reset()
		if code := Execute(context.Background(), append([]string{"view"}, flags...), nil, &out, &stderr); code != 2 || out.Len() != 0 {
			t.Fatal(flags, code, out.String(), stderr.String())
		}
	}
}
