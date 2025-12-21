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
