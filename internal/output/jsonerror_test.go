package output

import (
	"bytes"
	"testing"
)

func TestJSONError_GoesToErrWriter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewWithWriters(&stdout, &stderr, ModeJSON, false)

	if err := p.JSONError(2, "credentials not set", "set CLOUDRU_KEY_ID"); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	want := `{"error":{"code":2,"message":"credentials not set","hint":"set CLOUDRU_KEY_ID"}}` + "\n"
	if stderr.String() != want {
		t.Errorf("got %q, want %q", stderr.String(), want)
	}
}
