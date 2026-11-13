package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func makeTool(props map[string]any, required []string) mcp.Tool {
	return mcp.Tool{
		InputSchema: mcp.ToolInputSchema{
			Properties: props,
			Required:   required,
		},
	}
}

func TestParseToolFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		tool    mcp.Tool
		want    map[string]any
		wantErr bool
	}{
		{
			name: "string with equals",
			args: []string{"--foo=bar"},
			tool: makeTool(map[string]any{"foo": map[string]any{"type": "string"}}, nil),
			want: map[string]any{"foo": "bar"},
		},
		{
			name: "string with space",
			args: []string{"--foo", "bar"},
			tool: makeTool(map[string]any{"foo": map[string]any{"type": "string"}}, nil),
			want: map[string]any{"foo": "bar"},
		},
		{
			name: "integer",
			args: []string{"--num=42"},
			tool: makeTool(map[string]any{"num": map[string]any{"type": "integer"}}, nil),
			want: map[string]any{"num": int64(42)},
		},
		{
			name: "integer with space",
			args: []string{"--num", "42"},
			tool: makeTool(map[string]any{"num": map[string]any{"type": "integer"}}, nil),
			want: map[string]any{"num": int64(42)},
		},
		{
			name: "bool flag true",
			args: []string{"--verbose"},
			tool: makeTool(map[string]any{"verbose": map[string]any{"type": "boolean"}}, nil),
			want: map[string]any{"verbose": true},
		},
		{
			name: "bool flag negated",
			args: []string{"--no-verbose"},
			tool: makeTool(map[string]any{"verbose": map[string]any{"type": "boolean"}}, nil),
			want: map[string]any{"verbose": false},
		},
		{
			name: "bool explicit true",
			args: []string{"--verbose=true"},
			tool: makeTool(map[string]any{"verbose": map[string]any{"type": "boolean"}}, nil),
			want: map[string]any{"verbose": true},
		},
		{
			name: "bool explicit false",
			args: []string{"--verbose=false"},
			tool: makeTool(map[string]any{"verbose": map[string]any{"type": "boolean"}}, nil),
			want: map[string]any{"verbose": false},
		},
		{
			name: "array repeated flags",
			args: []string{"--tags=a", "--tags=b", "--tags=c"},
			tool: makeTool(map[string]any{"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, nil),
			want: map[string]any{"tags": []any{"a", "b", "c"}},
		},
		{
			name: "array with space format",
			args: []string{"--tags", "a", "--tags", "b"},
			tool: makeTool(map[string]any{"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, nil),
			want: map[string]any{"tags": []any{"a", "b"}},
		},
		{
			name: "mixed formats",
			args: []string{"--name=test", "--count", "5", "--verbose"},
			tool: makeTool(map[string]any{
				"name":    map[string]any{"type": "string"},
				"count":   map[string]any{"type": "integer"},
				"verbose": map[string]any{"type": "boolean"},
			}, nil),
			want: map[string]any{"name": "test", "count": int64(5), "verbose": true},
		},
		{
			name: "default applied",
			args: []string{},
			tool: makeTool(map[string]any{"foo": map[string]any{"type": "string", "default": "bar"}}, nil),
			want: map[string]any{"foo": "bar"},
		},
		{
			name: "null default satisfies required flag",
			tool: makeTool(map[string]any{"foo": map[string]any{
				"type": []any{"string", "null"}, "default": nil,
			}}, []string{"foo"}),
			want: map[string]any{"foo": nil},
		},
		{
			name: "explicit value overrides null default",
			args: []string{"--foo=bar"},
			tool: makeTool(map[string]any{"foo": map[string]any{
				"type": []any{"string", "null"}, "default": nil,
			}}, []string{"foo"}),
			want: map[string]any{"foo": "bar"},
		},
		{
			name:    "missing required",
			args:    []string{},
			tool:    makeTool(map[string]any{"foo": map[string]any{"type": "string"}}, []string{"foo"}),
			wantErr: true,
		},
		{
			name: "bool followed by string flag",
			args: []string{"--verbose", "--name=test"},
			tool: makeTool(map[string]any{
				"verbose": map[string]any{"type": "boolean"},
				"name":    map[string]any{"type": "string"},
			}, nil),
			want: map[string]any{"verbose": true, "name": "test"},
		},
		{
			name:    "unknown flag",
			args:    []string{"--unknown=value"},
			tool:    makeTool(map[string]any{"foo": map[string]any{"type": "string"}}, nil),
			wantErr: true,
		},
		{
			name:    "negated non-boolean",
			args:    []string{"--no-foo"},
			tool:    makeTool(map[string]any{"foo": map[string]any{"type": "string"}}, nil),
			wantErr: true,
		},
		{
			name:    "negated with value",
			args:    []string{"--no-verbose=true"},
			tool:    makeTool(map[string]any{"verbose": map[string]any{"type": "boolean"}}, nil),
			wantErr: true,
		},
		{
			name: "property literally named no-x",
			args: []string{"--no-cache=yes"},
			tool: makeTool(map[string]any{"no-cache": map[string]any{"type": "string"}}, nil),
			want: map[string]any{"no-cache": "yes"},
		},
		{
			name:    "missing value",
			args:    []string{"--foo"},
			tool:    makeTool(map[string]any{"foo": map[string]any{"type": "string"}}, nil),
			wantErr: true,
		},
		{
			name: "nullable union type",
			args: []string{"--num=7"},
			tool: makeTool(map[string]any{"num": map[string]any{"type": []any{"null", "integer"}}}, nil),
			want: map[string]any{"num": int64(7)},
		},
		{
			name: "array of integers",
			args: []string{"--ids=1", "--ids", "2"},
			tool: makeTool(map[string]any{"ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}}, nil),
			want: map[string]any{"ids": []any{int64(1), int64(2)}},
		},
		{
			name: "object as JSON",
			args: []string{`--params={"a":1}`},
			tool: makeTool(map[string]any{"params": map[string]any{"type": "object"}}, nil),
			want: map[string]any{"params": map[string]any{"a": json.Number("1")}},
		},
		{
			name: "object preserves large integers",
			args: []string{`--params={"id":9007199254740993}`},
			tool: makeTool(map[string]any{"params": map[string]any{"type": "object"}}, nil),
			want: map[string]any{"params": map[string]any{"id": json.Number("9007199254740993")}},
		},
		{
			name: "nested array",
			args: []string{`--matrix=[9007199254740993]`},
			tool: makeTool(map[string]any{"matrix": map[string]any{
				"type": "array", "items": map[string]any{"type": "array"},
			}}, nil),
			want: map[string]any{"matrix": []any{[]any{json.Number("9007199254740993")}}},
		},
		{
			name: "nested array rejects object",
			args: []string{`--matrix={}`},
			tool: makeTool(map[string]any{"matrix": map[string]any{
				"type": "array", "items": map[string]any{"type": "array"},
			}}, nil),
			wantErr: true,
		},
		{
			name:    "invalid integer",
			args:    []string{"--num=abc"},
			tool:    makeTool(map[string]any{"num": map[string]any{"type": "integer"}}, nil),
			wantErr: true,
		},
		{
			name:    "positional argument",
			args:    []string{"foo"},
			tool:    makeTool(map[string]any{"foo": map[string]any{"type": "string"}}, nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseToolFlags(tt.args, tt.tool)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseToolFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseToolFlags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseValueJSON(t *testing.T) {
	for _, typ := range []string{"object", "array"} {
		for _, input := range []string{`123`, `"text"`, `true`, `null`, `{`, `{} {}`, `[] []`, `{} trailing`} {
			t.Run(typ+"/"+input, func(t *testing.T) {
				if got, err := parseValue(typ, input); err == nil {
					t.Fatalf("parseValue(%q, %q) = %v, expected error", typ, input, got)
				}
			})
		}
	}
	for _, tt := range []struct {
		typ   string
		input string
	}{
		{"object", `{"id":9007199254740993,"nested":[1.234567890123456789,1e400]}`},
		{"array", `[9007199254740993,{"n":1.234567890123456789}]`},
		{"object", `{}`},
		{"array", `[]`},
	} {
		t.Run(tt.typ+"/"+tt.input, func(t *testing.T) {
			got, err := parseValue(tt.typ, tt.input+" \n")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != tt.input {
				t.Fatalf("JSON round trip = %s, want %s", encoded, tt.input)
			}
		})
	}
	for _, tt := range []struct{ typ, input string }{{"object", "[]"}, {"array", "{}"}} {
		if _, err := parseValue(tt.typ, tt.input); err == nil {
			t.Errorf("parseValue(%q, %q) should reject wrong container type", tt.typ, tt.input)
		}
	}
}

func TestParseValueNumber(t *testing.T) {
	for _, input := range []string{"NaN", "Inf", "+Inf", "-Inf", "Infinity", "1e400"} {
		if _, err := parseValue("number", input); err == nil {
			t.Errorf("parseValue(number, %q) should reject non-finite number", input)
		}
	}
	if got, err := parseValue("number", "3.14"); err != nil || got != 3.14 {
		t.Errorf("parseValue(number, 3.14) = %v, %v", got, err)
	}
}

// These tests must not run in parallel because stdout is process-global.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	defer func() {
		os.Stdout = old
		f.Close()
	}()
	os.Stdout = f
	fn()
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPrintResult(t *testing.T) {
	for _, tt := range []struct {
		name, text, want string
	}{
		{"large integer", "9007199254740993", "9007199254740993\n"},
		{"decimal", "1.234567890123456789", "1.234567890123456789\n"},
		{"large exponent", "1e400", "1e400\n"},
		{"object", `{"id":9007199254740993}`, "{\n  \"id\": 9007199254740993\n}\n"},
		{"JSON whitespace", " \n{\"n\":1}\n ", "{\n  \"n\": 1\n}\n"},
		{"plain text", "hello", "hello\n"},
		{"invalid JSON", "{not json}", "{not json}\n"},
		{"multiple values", "{} {}", "{} {}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := captureStdout(t, func() {
				printResult(mcp.NewToolResultText(tt.text))
			})
			if got != tt.want {
				t.Fatalf("printResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintToolHelpDefaults(t *testing.T) {
	tool := makeTool(map[string]any{
		"nullable": map[string]any{"type": []any{"string", "null"}, "default": nil},
		"count":    map[string]any{"type": "integer", "default": 3},
		"name":     map[string]any{"type": "string"},
	}, []string{"nullable", "count", "name"})
	got := captureStdout(t, func() { printToolHelp(tool) })
	for _, want := range []string{
		"--nullable <string> (default: null)\n",
		"--count <integer> (default: 3)\n",
		"--name <string> (required)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tool help %q does not contain %q", got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"this is too long", 10, "this is..."},
		{"first line\nsecond line", 60, "first line..."},
		{"first line.\nsecond line", 60, "first line..."},
		{"äöüäöüäöüäöü", 8, "äöüäö..."},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.max); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}
