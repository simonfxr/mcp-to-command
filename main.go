package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const progName = "mcp-to-command"

// version is overridden at build time via -ldflags "-X main.version=...".
var version = ""

func getVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: %[1]s <mcp-server-command> [server-args...] -- <tool-name> [--flag=value...]
       %[1]s <http(s)://mcp-server-url> -- <tool-name> [--flag=value...]

Executes MCP server tools via command line.

Examples:
  # Local MCP server via stdio
  %[1]s go-jenkins-mcp -url example.com -- jenkins_get_jobs
  %[1]s go-jenkins-mcp -url example.com -- jenkins_get_job --name=my-job

  # Remote MCP server via Streamable HTTP (URLs ending in /sse use the legacy SSE transport)
  %[1]s https://mcp.example.com/mcp -- jenkins_get_jobs

Use -- --help to list all available tools.
Use -- <tool-name> --help to see help for a specific tool.
`, progName)
}

// truncate shortens s to its first line and at most maxRunes runes.
func truncate(s string, maxRunes int) string {
	s, _, cut := strings.Cut(strings.TrimSpace(s), "\n")
	if utf8.RuneCountInString(s) > maxRunes {
		s = string([]rune(s)[:maxRunes-3])
		cut = true
	}
	if cut {
		s = strings.TrimRight(s, " .") + "..."
	}
	return s
}

func printToolsHelp(tools []mcp.Tool) {
	fmt.Println("Available tools:")
	fmt.Println()

	slices.SortFunc(tools, func(a, b mcp.Tool) int { return strings.Compare(a.Name, b.Name) })

	maxNameLen := 0
	for _, tool := range tools {
		maxNameLen = max(maxNameLen, len(tool.Name))
	}

	for _, tool := range tools {
		fmt.Printf("  %-*s  %s\n", maxNameLen, tool.Name, truncate(tool.Description, 60))
	}
	fmt.Println()
	fmt.Println("Use <tool-name> --help for more information about a tool.")
}

// schemaType returns the JSON Schema type of a property, defaulting to
// "string". For union types such as ["string", "null"] the first non-null
// type is used.
func schemaType(prop map[string]any) string {
	switch t := prop["type"].(type) {
	case string:
		if t != "" {
			return t
		}
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s != "null" {
				return s
			}
		}
	}
	return "string"
}

// arrayItemType returns the JSON Schema type of an array property's items.
func arrayItemType(prop map[string]any) string {
	if items, ok := prop["items"].(map[string]any); ok {
		return schemaType(items)
	}
	return "string"
}

func printToolHelp(tool mcp.Tool) {
	fmt.Printf("%s\n\n", tool.Name)
	if desc := strings.TrimSpace(tool.Description); desc != "" {
		fmt.Printf("  %s\n\n", strings.ReplaceAll(desc, "\n", "\n  "))
	}

	if len(tool.InputSchema.Properties) == 0 {
		fmt.Println("  This tool takes no arguments.")
		return
	}

	required := make(map[string]bool, len(tool.InputSchema.Required))
	for _, r := range tool.InputSchema.Required {
		required[r] = true
	}

	// Required flags first, then optional ones, alphabetically within each group.
	names := make([]string, 0, len(tool.InputSchema.Properties))
	for name := range tool.InputSchema.Properties {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if required[a] != required[b] {
			if required[a] {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})

	fmt.Println("Flags:")

	for _, name := range names {
		prop, ok := tool.InputSchema.Properties[name].(map[string]any)
		if !ok {
			continue
		}

		var suffix string
		def, hasDefault := prop["default"]
		if required[name] && !hasDefault {
			suffix += " (required)"
		}
		if hasDefault {
			if def == nil {
				suffix += " (default: null)"
			} else {
				suffix += fmt.Sprintf(" (default: %v)", def)
			}
		}

		switch typ := schemaType(prop); typ {
		case "boolean":
			fmt.Printf("  --%s[=<bool>], --no-%s%s\n", name, name, suffix)
		case "array":
			fmt.Printf("  --%s <%s> (repeatable)%s\n", name, arrayItemType(prop), suffix)
		default:
			fmt.Printf("  --%s <%s>%s\n", name, typ, suffix)
		}

		if desc, ok := prop["description"].(string); ok && desc != "" {
			fmt.Printf("      %s\n", desc)
		}
	}
}

// parseValue converts a CLI string into a value of the given JSON Schema type.
func parseValue(typ, s string) (any, error) {
	switch typ {
	case "integer":
		return strconv.ParseInt(s, 10, 64)
	case "number":
		v, err := strconv.ParseFloat(s, 64)
		if err == nil && (math.IsNaN(v) || math.IsInf(v, 0)) {
			return nil, fmt.Errorf("requires a finite number")
		}
		return v, err
	case "boolean":
		return strconv.ParseBool(s)
	case "object", "array":
		var v any
		decoder := json.NewDecoder(strings.NewReader(s))
		decoder.UseNumber()
		if err := decoder.Decode(&v); err != nil {
			return nil, fmt.Errorf("requires valid JSON: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("requires a single JSON value")
		}
		if _, ok := v.(map[string]any); typ == "object" && !ok {
			return nil, fmt.Errorf("requires a JSON object")
		}
		if _, ok := v.([]any); typ == "array" && !ok {
			return nil, fmt.Errorf("requires a JSON array")
		}
		return v, nil
	default:
		return s, nil
	}
}

// lookupFlag resolves a flag name against the tool schema, handling the
// --no-<name> form for boolean properties.
func lookupFlag(tool mcp.Tool, name string) (resolved string, prop map[string]any, negated bool, err error) {
	raw, ok := tool.InputSchema.Properties[name]
	if !ok {
		if base, isNo := strings.CutPrefix(name, "no-"); isNo {
			if p, ok := tool.InputSchema.Properties[base].(map[string]any); ok && schemaType(p) == "boolean" {
				return base, p, true, nil
			}
		}
		return "", nil, false, fmt.Errorf("unknown flag: --%s", name)
	}
	prop, ok = raw.(map[string]any)
	if !ok {
		return "", nil, false, fmt.Errorf("invalid schema for flag: --%s", name)
	}
	return name, prop, false, nil
}

func parseToolFlags(args []string, tool mcp.Tool) (map[string]any, error) {
	result := make(map[string]any)

	for i := 0; i < len(args); i++ {
		arg, ok := strings.CutPrefix(args[i], "--")
		if !ok {
			return nil, fmt.Errorf("unexpected argument: %s (flags must start with --)", args[i])
		}

		name, value, hasValue := strings.Cut(arg, "=")

		name, prop, negated, err := lookupFlag(tool, name)
		if err != nil {
			return nil, err
		}
		typ := schemaType(prop)

		if negated {
			if hasValue {
				return nil, fmt.Errorf("flag --no-%s takes no value", name)
			}
			result[name] = false
			continue
		}

		if !hasValue {
			if typ == "boolean" {
				result[name] = true
				continue
			}
			// Consume the next argument as the value.
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, fmt.Errorf("flag --%s requires a value", name)
			}
			i++
			value = args[i]
		}

		if typ == "array" {
			item, err := parseValue(arrayItemType(prop), value)
			if err != nil {
				return nil, fmt.Errorf("flag --%s invalid item value: %w", name, err)
			}
			list, _ := result[name].([]any)
			result[name] = append(list, item)
			continue
		}

		val, err := parseValue(typ, value)
		if err != nil {
			return nil, fmt.Errorf("flag --%s: %w", name, err)
		}
		result[name] = val
	}

	// Apply defaults for missing flags.
	for name, raw := range tool.InputSchema.Properties {
		if _, set := result[name]; set {
			continue
		}
		if prop, ok := raw.(map[string]any); ok {
			if def, ok := prop["default"]; ok {
				result[name] = def
			}
		}
	}

	for _, req := range tool.InputSchema.Required {
		if _, set := result[req]; !set {
			return nil, fmt.Errorf("missing required flag: --%s", req)
		}
	}

	return result, nil
}

func findTool(tools []mcp.Tool, name string) *mcp.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// connect creates and starts an MCP client for either a remote URL or a
// local stdio server command.
func connect(ctx context.Context, serverArgs []string) (*client.Client, error) {
	var (
		c   *client.Client
		err error
	)
	if isHTTPURL(serverArgs[0]) {
		if len(serverArgs) > 1 {
			return nil, fmt.Errorf("HTTP URL mode does not accept additional server arguments")
		}
		u, perr := url.Parse(serverArgs[0])
		if perr != nil {
			return nil, fmt.Errorf("invalid URL: %w", perr)
		}
		if strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/sse") {
			c, err = client.NewSSEMCPClient(serverArgs[0])
		} else {
			c, err = client.NewStreamableHttpClient(serverArgs[0])
		}
		if err != nil {
			return nil, fmt.Errorf("creating HTTP client: %w", err)
		}
	} else {
		c, err = client.NewStdioMCPClient(serverArgs[0], nil, serverArgs[1:]...)
		if err != nil {
			return nil, fmt.Errorf("starting MCP server: %w", err)
		}
	}

	if err := c.Start(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("starting MCP transport: %w", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    progName,
		Version: getVersion(),
	}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		c.Close()
		return nil, fmt.Errorf("initializing MCP connection: %w", err)
	}

	return c, nil
}

func printJSON(w io.Writer, v any) {
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "%v\n", v)
		return
	}
	fmt.Fprintln(w, string(pretty))
}

func run(args []string) int {
	if len(args) == 1 {
		switch args[0] {
		case "--version", "-version":
			fmt.Printf("%s %s\n", progName, getVersion())
			return 0
		case "--help", "-help", "-h":
			printUsage()
			return 0
		}
	}

	separatorIdx := slices.Index(args, "--")
	if separatorIdx == -1 {
		printUsage()
		return 1
	}

	serverArgs := args[:separatorIdx]
	toolArgs := args[separatorIdx+1:]

	if len(serverArgs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: MCP server command or URL is required")
		printUsage()
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mcpClient, err := connect(ctx, serverArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer mcpClient.Close()

	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tools: %v\n", err)
		return 1
	}
	tools := toolsResult.Tools

	if len(toolArgs) == 0 || (len(toolArgs) == 1 && toolArgs[0] == "--help") {
		printToolsHelp(tools)
		return 0
	}

	toolName, toolFlags := toolArgs[0], toolArgs[1:]

	tool := findTool(tools, toolName)
	if tool == nil {
		fmt.Fprintf(os.Stderr, "Error: unknown tool '%s'\n\n", toolName)
		printToolsHelp(tools)
		return 1
	}

	if slices.Contains(toolFlags, "--help") {
		printToolHelp(*tool)
		return 0
	}

	arguments, err := parseToolFlags(toolFlags, *tool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printToolHelp(*tool)
		return 1
	}

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = toolName
	callReq.Params.Arguments = arguments
	result, err := mcpClient.CallTool(ctx, callReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error calling tool: %v\n", err)
		return 1
	}

	printer := resultPrinter{out: os.Stdout, warn: os.Stderr}
	if mcpClient.GetServerCapabilities().Resources != nil {
		printer.resources = mcpClient
	}

	if result.IsError {
		printer.out = os.Stderr
		printer.print(ctx, result)
		return 1
	}

	printer.print(ctx, result)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:]))
}
