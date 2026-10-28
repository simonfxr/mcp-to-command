package main

import (
	"reflect"
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
			want: map[string]any{"params": map[string]any{"a": float64(1)}},
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
