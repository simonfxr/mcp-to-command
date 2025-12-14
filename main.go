package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// MCP JSON-RPC types
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP Tool types
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// MCP client that communicates via stdio
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	reqID  int
}

func NewMCPClient(command string, args []string) (*MCPClient, error) {
	cmd := exec.Command(command, args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start MCP server: %w", err)
	}

	return &MCPClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		reqID:  0,
	}, nil
}

func (c *MCPClient) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

func (c *MCPClient) sendRequest(method string, params interface{}) (json.RawMessage, error) {
	c.reqID++
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.reqID,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response line
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

func (c *MCPClient) Initialize() error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "mcp-to-command",
			"version": "1.0.0",
		},
	}

	_, err := c.sendRequest("initialize", params)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	// Send initialized notification (no ID for notifications)
	notif := map[string]string{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	data, _ := json.Marshal(notif)
	c.stdin.Write(append(data, '\n'))

	return nil
}

func (c *MCPClient) ListTools() ([]Tool, error) {
	result, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, err
	}

	var toolsResult ToolsListResult
	if err := json.Unmarshal(result, &toolsResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tools: %w", err)
	}

	return toolsResult.Tools, nil
}

func (c *MCPClient) CallTool(name string, arguments map[string]interface{}) (json.RawMessage, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	return c.sendRequest("tools/call", params)
}

// CLI help output

func printUsage(progName string) {
	fmt.Fprintf(os.Stderr, `Usage: %s <mcp-server-command> [server-args...] -- <tool-name> [--flag=value...]

Executes MCP server tools via command line.

Examples:
  %s go-jenkins-mcp -url example.com -- jenkins_get_jobs
  %s go-jenkins-mcp -url example.com -- jenkins_get_job --name=my-job
  %s go-jenkins-mcp -url example.com -- --help

Use -- --help to list all available tools.
Use -- <tool-name> --help to see help for a specific tool.
`, progName, progName, progName, progName)
}

func printToolsHelp(tools []Tool) {
	fmt.Println("Available tools:")
	fmt.Println()

	// Sort tools by name
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	maxNameLen := 0
	for _, tool := range tools {
		if len(tool.Name) > maxNameLen {
			maxNameLen = len(tool.Name)
		}
	}

	for _, tool := range tools {
		desc := tool.Description
		// Truncate long descriptions
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("  %-*s  %s\n", maxNameLen, tool.Name, desc)
	}
	fmt.Println()
	fmt.Println("Use <tool-name> --help for more information about a tool.")
}

func printToolHelp(tool Tool) {
	fmt.Printf("%s\n\n", tool.Name)
	fmt.Printf("  %s\n\n", tool.Description)

	if len(tool.InputSchema.Properties) == 0 {
		fmt.Println("  This tool takes no arguments.")
		return
	}

	// Build required set
	requiredSet := make(map[string]bool)
	for _, r := range tool.InputSchema.Required {
		requiredSet[r] = true
	}

	// Sort properties: required first, then optional, alphabetically within each group
	var propNames []string
	for name := range tool.InputSchema.Properties {
		propNames = append(propNames, name)
	}
	sort.Slice(propNames, func(i, j int) bool {
		iReq := requiredSet[propNames[i]]
		jReq := requiredSet[propNames[j]]
		if iReq != jReq {
			return iReq // required comes first
		}
		return propNames[i] < propNames[j]
	})

	fmt.Println("Flags:")

	for _, name := range propNames {
		prop := tool.InputSchema.Properties[name]
		required := requiredSet[name]

		flagName := fmt.Sprintf("--%s", name)
		typeStr := prop.Type
		if typeStr == "" {
			typeStr = "string"
		}

		reqStr := ""
		if required {
			reqStr = " (required)"
		}

		defaultStr := ""
		if prop.Default != nil {
			defaultStr = fmt.Sprintf(" (default: %v)", prop.Default)
		}

		fmt.Printf("  %s <%s>%s%s\n", flagName, typeStr, reqStr, defaultStr)
		if prop.Description != "" {
			fmt.Printf("      %s\n", prop.Description)
		}
		if typeStr == "object" {
			fmt.Printf("      Example: %s='{\"key\":\"value\"}'\n", flagName)
		}
		if typeStr == "array" {
			fmt.Printf("      Example: %s='[\"item1\",\"item2\"]'\n", flagName)
		}
	}
}

// Flag parsing

func parseToolFlags(args []string, tool Tool) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// Apply defaults first
	for name, prop := range tool.InputSchema.Properties {
		if prop.Default != nil {
			result[name] = prop.Default
		}
	}

	// Parse flags
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unexpected argument: %s (flags must start with --)", arg)
		}

		arg = strings.TrimPrefix(arg, "--")
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid flag format: --%s (expected --name=value)", arg)
		}

		name := parts[0]
		value := parts[1]

		prop, exists := tool.InputSchema.Properties[name]
		if !exists {
			return nil, fmt.Errorf("unknown flag: --%s", name)
		}

		// Convert value based on type
		switch prop.Type {
		case "integer":
			intVal, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("flag --%s requires an integer value: %w", name, err)
			}
			result[name] = intVal
		case "number":
			floatVal, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("flag --%s requires a number value: %w", name, err)
			}
			result[name] = floatVal
		case "boolean":
			boolVal, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("flag --%s requires a boolean value: %w", name, err)
			}
			result[name] = boolVal
		case "object", "array":
			var jsonVal interface{}
			if err := json.Unmarshal([]byte(value), &jsonVal); err != nil {
				return nil, fmt.Errorf("flag --%s requires valid JSON: %w", name, err)
			}
			result[name] = jsonVal
		default:
			result[name] = value
		}
	}

	// Check required fields
	for _, req := range tool.InputSchema.Required {
		if _, exists := result[req]; !exists {
			return nil, fmt.Errorf("missing required flag: --%s", req)
		}
	}

	return result, nil
}

func findTool(tools []Tool, name string) *Tool {
	for _, tool := range tools {
		if tool.Name == name {
			return &tool
		}
	}
	return nil
}

// MCP tool result types
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage(os.Args[0])
		os.Exit(1)
	}

	// Find the -- separator
	separatorIdx := -1
	for i, arg := range args {
		if arg == "--" {
			separatorIdx = i
			break
		}
	}

	if separatorIdx == -1 {
		printUsage(os.Args[0])
		os.Exit(1)
	}

	serverArgs := args[:separatorIdx]
	toolArgs := args[separatorIdx+1:]

	if len(serverArgs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: MCP server command is required")
		printUsage(os.Args[0])
		os.Exit(1)
	}

	serverCmd := serverArgs[0]
	serverCmdArgs := serverArgs[1:]

	// Start MCP client
	client, err := NewMCPClient(serverCmd, serverCmdArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting MCP server: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Initialize
	if err := client.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing MCP connection: %v\n", err)
		os.Exit(1)
	}

	// Get tools list
	tools, err := client.ListTools()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tools: %v\n", err)
		os.Exit(1)
	}

	// No tool specified or --help: show tools list
	if len(toolArgs) == 0 || (len(toolArgs) == 1 && toolArgs[0] == "--help") {
		printToolsHelp(tools)
		os.Exit(0)
	}

	toolName := toolArgs[0]
	toolFlags := toolArgs[1:]

	// Find the requested tool
	tool := findTool(tools, toolName)
	if tool == nil {
		fmt.Fprintf(os.Stderr, "Error: unknown tool '%s'\n\n", toolName)
		printToolsHelp(tools)
		os.Exit(1)
	}

	// Tool --help
	if len(toolFlags) == 1 && toolFlags[0] == "--help" {
		printToolHelp(*tool)
		os.Exit(0)
	}

	// Parse flags and call tool
	arguments, err := parseToolFlags(toolFlags, *tool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printToolHelp(*tool)
		os.Exit(1)
	}

	result, err := client.CallTool(toolName, arguments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error calling tool: %v\n", err)
		os.Exit(1)
	}

	// Try to parse as ToolResult and extract text content
	var toolResult ToolResult
	if err := json.Unmarshal(result, &toolResult); err == nil && len(toolResult.Content) > 0 {
		// Output each text content item
		for _, item := range toolResult.Content {
			if item.Type == "text" && item.Text != "" {
				// Try to pretty-print if it's JSON
				var jsonData interface{}
				if err := json.Unmarshal([]byte(item.Text), &jsonData); err == nil {
					pretty, _ := json.MarshalIndent(jsonData, "", "  ")
					fmt.Println(string(pretty))
				} else {
					fmt.Println(item.Text)
				}
			}
		}
		if toolResult.IsError {
			os.Exit(1)
		}
	} else {
		// Fallback: pretty print raw result
		var prettyResult interface{}
		if err := json.Unmarshal(result, &prettyResult); err != nil {
			fmt.Println(string(result))
		} else {
			output, _ := json.MarshalIndent(prettyResult, "", "  ")
			fmt.Println(string(output))
		}
	}
}
