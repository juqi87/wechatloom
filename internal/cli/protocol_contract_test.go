package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wechatloom/wechatloom/internal/cli"
)

func TestPublicJSONResponsesConformToThePublishedEnvelopeSchema(t *testing.T) {
	t.Parallel()

	schemaBytes, err := os.ReadFile(filepath.Join("..", "..", "schemas", "protocol-envelope.schema.json"))
	if err != nil {
		t.Fatalf("read published protocol schema: %v", err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("decode published protocol schema: %v", err)
	}
	wantRequired := []string{"success", "code", "message", "schema_version", "status", "retryable", "warnings", "data"}
	if !equalStrings(schema.Required, wantRequired) {
		t.Fatalf("schema required fields = %v, want stable envelope fields %v", schema.Required, wantRequired)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "success", args: []string{"capabilities", "--json"}},
		{name: "error", args: []string{"theme", "show", "missing-theme", "--json"}},
		{name: "invalid arguments", args: []string{"theme", "--json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cli.NewRunner(&stdout, &stderr).Run(test.args)
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not a JSON envelope: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			for _, field := range schema.Required {
				if _, ok := envelope[field]; !ok {
					t.Errorf("JSON envelope is missing schema-required field %q: %s", field, stdout.String())
				}
			}
		})
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
