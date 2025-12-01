package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveJSONComments(t *testing.T) {
	src := []byte(`{
  // line comment
  "a": "value", /* block comment */
  "b": 123
}`)

	cleaned := removeJSONComments(src)

	var out map[string]any
	if err := json.Unmarshal(cleaned, &out); err != nil {
		t.Fatalf("cleaned JSON should unmarshal: %v\ncleaned: %s", err, string(cleaned))
	}

	if out["a"] != "value" {
		t.Fatalf("expected a=value, got %v", out["a"])
	}
}

func TestProcessFileAndParser(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "appsettings.json")

	src := `{
  // example settings
  "Logging": {
    "LogLevel": {
      "Default": "Information",
      "System": "Warning"
    },
    "Rules": ["Rule1", {"Name": "Rule2"}]
  },
  "Allowed": [1, 2, 3]
}`

	if err := os.WriteFile(fn, []byte(src), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	vars, err := processFile(fn, "__")
	if err != nil {
		t.Fatalf("processFile failed: %v", err)
	}

	cases := map[string]string{
		"Logging__LogLevel__Default": "Information",
		"Logging__LogLevel__System":  "Warning",
		"Logging__Rules__0":          "Rule1",
		"Logging__Rules__1__Name":    "Rule2",
		"Allowed__0":                 "1",
		"Allowed__1":                 "2",
		"Allowed__2":                 "3",
	}

	for k, want := range cases {
		v, ok := vars[k]
		if !ok {
			t.Fatalf("missing key %q", k)
		}
		if v != want {
			t.Fatalf("key %q: want %q got %q", k, want, v)
		}
	}
}

func TestProcessFileSyntaxError(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "bad.json")

	// malformed JSON
	src := `{
  "a": "b",
  "c": [1,2, // trailing comma causes syntax error
}`
	if err := os.WriteFile(fn, []byte(src), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	if _, err := processFile(fn, "__"); err == nil {
		t.Fatalf("expected syntax error, got nil")
	}
}
