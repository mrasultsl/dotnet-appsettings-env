package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	app         = "dotnet-appsettings-env"
	version     = "dev"
	description = "Convert .NET appsettings.json file to Kubernetes, Docker, Docker-Compose and Bicep environment variables."
	site        = "https://github.com/dassump/dotnet-appsettings-env"

	file      = flag.String("file", "./appsettings.json", "Path to file appsettings.json (supports globbing)")
	output    = flag.String("type", "k8s", "Output type: k8s|docker|compose|bicep")
	separator = flag.String("separator", "__", "Separator character(s)")
)

var format = map[string]string{
	"k8s":     "- name: %q\n  value: %q\n",
	"docker":  "%s=%q\n",
	"compose": "%s: %q\n",
	"bicep":   "{\nname: '%s'\nvalue: '%s'\n}\n",
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s (%s)\n\n%s\n%s\n\n", app, version, description, site)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	outType := strings.ToLower(strings.TrimSpace(*output))
	if _, ok := format[outType]; !ok {
		fmt.Fprintf(os.Stderr, "invalid output type: %q\n", *output)
		os.Exit(2)
	}

	if len(*separator) < 1 {
		fmt.Fprintln(os.Stderr, "separator cannot be an empty string")
		os.Exit(2)
	}

	files, err := filepath.Glob(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to evaluate file pattern: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no files matching pattern: %s\n", *file)
		os.Exit(1)
	}

	// Aggregate variables across matching files
	variables := make(map[string]string)
	hadErr := false
	for _, f := range files {
		m, err := processFile(f, *separator)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error processing %s: %v\n", f, err)
			hadErr = true
			continue
		}
		maps.Copy(variables, m)
	}

	if hadErr {
		os.Exit(1)
	}

	// Sort keys case-insensitively
	keys := make([]string, 0, len(variables))
	for k := range variables {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.ToLower(keys[i]) < strings.ToLower(keys[j])
	})

	// Print using requested format
	fmtStr := format[outType]
	for _, k := range keys {
		fmt.Printf(fmtStr, k, variables[k])
	}
}

// processFile reads, cleans and parses a single JSON file and returns flattened variables
func processFile(filename, sep string) (map[string]string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	// Remove BOM if present
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}

	// Remove JSON comments
	content = removeJSONComments(content)

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()

	var objs map[string]any
	if err := decoder.Decode(&objs); err != nil {
		// Provide contextual error for syntax errors
		var synErr *json.SyntaxError
		if errors.As(err, &synErr) {
			offset := max(int(synErr.Offset), 0)
			before := max(offset-60, 0)
			after := offset + 60
			if after > len(content) {
				after = len(content)
			}

			// compute line and column
			line := bytes.Count(content[:offset], []byte("\n")) + 1
			prev := bytes.LastIndex(content[:offset], []byte("\n"))
			col := offset - prev

			snippet := content[before:after]
			return nil, fmt.Errorf("syntax error: %v in %s (line %d, column %d) ... %s", synErr, filename, line, col, snippet)
		}
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	out := make(map[string]string)
	parser(objs, out, nil, sep)
	return out, nil
}

// parser flattens nested JSON objects/arrays into environment-style variables using separator
func parser(in map[string]any, out map[string]string, root []string, sep string) {
	for key, value := range in {
		keys := append(root, key)

		switch v := value.(type) {
		case []any:
			for idx, item := range v {
				switch item := item.(type) {
				case []any:
					parser(map[string]any{fmt.Sprint(idx): item}, out, keys, sep)
				case map[string]any:
					parser(item, out, append(keys, fmt.Sprint(idx)), sep)
				default:
					base := strings.Join(keys, sep)
					out[fmt.Sprintf("%s%s%d", base, sep, idx)] = fmt.Sprint(item)
				}
			}
		case map[string]any:
			parser(v, out, keys, sep)
		default:
			out[strings.Join(keys, sep)] = fmt.Sprint(v)
		}
	}
}

// removeJSONComments removes single-line (//) and multi-line (/* */) comments from JSON content
func removeJSONComments(content []byte) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, len(content)))
	inString := false
	escapeNext := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(content); i++ {
		ch := content[i]

		if inString {
			buf.WriteByte(ch)
			if escapeNext {
				escapeNext = false
				continue
			}
			if ch == '\\' {
				escapeNext = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				buf.WriteByte(ch)
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < len(content) && content[i+1] == '/' {
				inBlockComment = false
				i++
				continue
			}
			continue
		}

		if ch == '"' {
			inString = true
			buf.WriteByte(ch)
			continue
		}

		if ch == '/' && i+1 < len(content) && content[i+1] == '/' {
			inLineComment = true
			i++
			continue
		}

		if ch == '/' && i+1 < len(content) && content[i+1] == '*' {
			inBlockComment = true
			i++
			continue
		}

		buf.WriteByte(ch)
	}

	return buf.Bytes()
}
