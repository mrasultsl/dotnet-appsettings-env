package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestRemoveJSONComments_BOMAndEscaping(t *testing.T) {
	// Write a file that starts with a BOM and contains escaped quotes and comment-like sequences inside strings
	src := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{
  "path": "C:\\Program Files\\App\"Name\"",
  "url": "http://example.com//not-a-comment",
  "note": "this is /* not a comment */ still text"
}`)...)

	dir := t.TempDir()
	fn := filepath.Join(dir, "bom.json")
	if err := os.WriteFile(fn, src, 0o644); err != nil {
		t.Fatalf("write bom file: %v", err)
	}

	vars, err := processFile(fn, "__")
	if err != nil {
		t.Fatalf("processFile failed on BOM file: %v", err)
	}

	if vars["url"] != "http://example.com//not-a-comment" {
		t.Fatalf("url changed: %v", vars["url"])
	}

	if vars["note"] != "this is /* not a comment */ still text" {
		t.Fatalf("note changed: %v", vars["note"])
	}
}

func TestProcessFile_LargeNestedJSON(t *testing.T) {
	// Build a deep nested object programmatically
	depth := 150
	root := make(map[string]any)
	cur := root
	var keys []string
	for i := 0; i < depth; i++ {
		k := fmt.Sprintf("k%d", i)
		keys = append(keys, k)
		next := make(map[string]any)
		cur[k] = next
		cur = next
	}
	// set final value
	cur["leaf"] = "deep-value"

	// marshal to JSON
	b, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("marshal nested: %v", err)
	}

	// write to temp file
	dir := t.TempDir()
	fn := filepath.Join(dir, "deep.json")
	if err := os.WriteFile(fn, b, 0o644); err != nil {
		t.Fatalf("write deep file: %v", err)
	}

	vars, err := processFile(fn, "__")
	if err != nil {
		t.Fatalf("processFile deep failed: %v", err)
	}

	// build expected key
	expectedKey := ""
	for i, k := range keys {
		if i == 0 {
			expectedKey = k
			continue
		}
		expectedKey = expectedKey + "__" + k
	}
	expectedKey = expectedKey + "__leaf"

	v, ok := vars[expectedKey]
	if !ok {
		t.Fatalf("missing deep key %q", expectedKey)
	}
	if v != "deep-value" {
		t.Fatalf("deep value mismatch: %q", v)
	}
}

func TestRemoveJSONComments_CommentLikeInString(t *testing.T) {
	src := []byte(`{"text":"contains // and /* not a comment */ and \\\"quotes\\\""}`)
	cleaned := removeJSONComments(src)
	var out map[string]any
	if err := json.Unmarshal(cleaned, &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	s, _ := out["text"].(string)
	if !strings.Contains(s, "//") || !strings.Contains(s, "/*") {
		t.Fatalf("string lost comment-like sequences: %q", s)
	}
	if !strings.Contains(s, "quotes") || !strings.Contains(s, `"`) {
		t.Fatalf("escaped quotes missing or lost: %q", s)
	}
}
