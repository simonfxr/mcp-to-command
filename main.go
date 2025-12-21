package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func printUsage(progName string) {
	fmt.Fprintf(os.Stderr, `Usage: %s <mcp-server-command> [server-args...] -- <tool-name> [--flag=value...]
       %s <http(s)://mcp-server-url> -- <tool-name> [--flag=value...]

Executes MCP server tools via command line.

Examples:
  # Local MCP server via stdio
  %s go-jenkins-mcp -url example.com -- jenkins_get_jobs
  %s go-jenkins-mcp -url example.com -- jenkins_get_job --name=my-job

  # Remote MCP server via HTTP
  %s https://mcp.example.com/api -- jenkins_get_jobs

Use -- --help to list all available tools.
Use -- <tool-name> --help to see help for a specific tool.
`, progName, progName, progName, progName, progName)
}

func printToolsHelp(tools []mcp.Tool) {
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

func printToolHelp(tool mcp.Tool) {
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
		propVal, ok := tool.InputSchema.Properties[name].(map[string]any)
		if !ok {
			// Try as map[string]string just in case, though json usually gives any
			continue
		}

		required := requiredSet[name]

		flagName := fmt.Sprintf("--%s", name)

		typeStr, _ := propVal["type"].(string)
		if typeStr == "" {
			typeStr = "string"
		}

		reqStr := ""
		if required {
			reqStr = " (required)"
		}

		defaultStr := ""
		if def, ok := propVal["default"]; ok && def != nil {
			defaultStr = fmt.Sprintf(" (default: %v)", def)
		}

		switch typeStr {
		case "boolean":
			fmt.Printf("  %s[=<value>], --no-%s%s%s\n", flagName, name, reqStr, defaultStr)
		case "array":
			fmt.Printf("  %s <value> (repeatable)%s%s\n", flagName, reqStr, defaultStr)
		default:
			fmt.Printf("  %s <value>%s%s\n", flagName, reqStr, defaultStr)
		}

		if desc, ok := propVal["description"].(string); ok && desc != "" {
			fmt.Printf("      %s\n", desc)
		}
	}
}

// Flag parsing

func parseToolFlags(args []string, tool mcp.Tool) (map[string]any, error) {
	result := make(map[string]any)

	// Parse flags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unexpected argument: %s (flags must start with --)", arg)
		}

		arg = strings.TrimPrefix(arg, "--")
		var name, value string
		var hasValue bool

		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			name = parts[0]
			value = parts[1]
			hasValue = true
		} else {
			name = arg
			hasValue = false
		}

		// Resolve flag name (handle --no-prefix)
		propVal, exists := tool.InputSchema.Properties[name]
		var isNegated bool
		if !exists && strings.HasPrefix(name, "no-") {
			candidate := strings.TrimPrefix(name, "no-")
			if p, ok := tool.InputSchema.Properties[candidate]; ok {
				name = candidate
				propVal = p
				exists = true
				isNegated = true
			}
		}

		if !exists {
			return nil, fmt.Errorf("unknown flag: --%s", name)
		}

		prop, ok := propVal.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid schema for flag: --%s", name)
		}

		propType, _ := prop["type"].(string)

		// For non-boolean types, consume next arg as value if no = was used
		if !hasValue && propType != "boolean" {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				value = args[i+1]
				hasValue = true
				i++
			}
		}

		// Handle boolean shorthand (no value provided)
		if !hasValue {
			if propType == "boolean" {
				if isNegated {
					result[name] = false
				} else {
					result[name] = true
				}
				continue
			} else {
				return nil, fmt.Errorf("flag --%s requires a value", name)
			}
		}

		if isNegated {
			if propType != "boolean" {
				return nil, fmt.Errorf("flag --no-%s is not valid for non-boolean type", name)
			}
			if hasValue {
				return nil, fmt.Errorf("flag --no-%s takes no value", name)
			}
		}

		// Helper to parse value based on type string
		parseValue := func(typeStr string, valStr string) (any, error) {
			switch typeStr {
			case "integer":
				return strconv.ParseInt(valStr, 10, 64)
			case "number":
				return strconv.ParseFloat(valStr, 64)
			case "boolean":
				return strconv.ParseBool(valStr)
			case "object", "array":
				var jsonVal any
				if err := json.Unmarshal([]byte(valStr), &jsonVal); err != nil {
					return nil, fmt.Errorf("requires valid JSON: %w", err)
				}
				return jsonVal, nil
			default:
				return valStr, nil
			}
		}

		if propType == "array" {
			// Initialize slice if first time seen
			if result[name] == nil {
				result[name] = []any{}
			}

			// Determine item type
			itemType := "string" // default
			if items, ok := prop["items"].(map[string]any); ok {
				if t, ok := items["type"].(string); ok {
					itemType = t
				}
			}

			// Parse item value
			itemVal, err := parseValue(itemType, value)
			if err != nil {
				return nil, fmt.Errorf("flag --%s invalid item value: %w", name, err)
			}
			
			// Append to existing slice
			if list, ok := result[name].([]any); ok {
				result[name] = append(list, itemVal)
			} else {
				// Should not happen if logic is correct
				result[name] = []any{itemVal}
			}

		} else {
			// Scalar types
			val, err := parseValue(propType, value)
			if err != nil {
				return nil, fmt.Errorf("flag --%s error: %w", name, err)
			}
			result[name] = val
		}
	}

	// Apply defaults for missing flags
	for name, propVal := range tool.InputSchema.Properties {
		if _, exists := result[name]; !exists {
			if prop, ok := propVal.(map[string]any); ok {
				if def, ok := prop["default"]; ok && def != nil {
					result[name] = def
				}
			}
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

func findTool(tools []mcp.Tool, name string) *mcp.Tool {
	for _, tool := range tools {
		if tool.Name == name {
			return &tool
		}
	}
	return nil
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
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
		fmt.Fprintln(os.Stderr, "Error: MCP server command or URL is required")
		printUsage(os.Args[0])
		os.Exit(1)
	}

	ctx := context.Background()
	var mcpClient client.MCPClient
	var err error

	if isHTTPURL(serverArgs[0]) {

		if len(serverArgs) > 1 {
			fmt.Fprintln(os.Stderr, "Error: HTTP URL mode does not accept additional server arguments")
			os.Exit(1)
		}

		// For SSE client
		mcpClient, err = client.NewSSEMCPClient(serverArgs[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating SSE client: %v\n", err)
			os.Exit(1)
		}
	} else {
		mcpClient, err = client.NewStdioMCPClient(serverArgs[0], nil, serverArgs[1:]...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting MCP server: %v\n", err)
			os.Exit(1)
		}
	}

	defer mcpClient.Close()

	// Initialize
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "mcp-to-command",
		Version: "1.0.0",
	}

	if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing MCP connection: %v\n", err)
		os.Exit(1)
	}

	// Get tools list
	listReq := mcp.ListToolsRequest{}
	toolsResult, err := mcpClient.ListTools(ctx, listReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing tools: %v\n", err)
		os.Exit(1)
	}

	tools := toolsResult.Tools
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

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = toolName
	callReq.Params.Arguments = arguments
	result, err := mcpClient.CallTool(ctx, callReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error calling tool: %v\n", err)
		os.Exit(1)
	}

	// Handle Result
	if result.IsError {
		// Print error content
		for _, content := range result.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				fmt.Fprintln(os.Stderr, textContent.Text)
			}
		}
		os.Exit(1)
	}

	// Print content
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			// Try to pretty-print if it's JSON
			var jsonData any
			if err := json.Unmarshal([]byte(textContent.Text), &jsonData); err == nil {
				pretty, _ := json.MarshalIndent(jsonData, "", "  ")
				fmt.Println(string(pretty))
			} else {
				fmt.Println(textContent.Text)
			}
		} else if imageContent, ok := content.(mcp.ImageContent); ok {
			fmt.Printf("[Image: %s (mime: %s)]\n", imageContent.Data, imageContent.MIMEType)
		} else if embeddedResource, ok := content.(mcp.EmbeddedResource); ok {
			var uri string
			switch res := embeddedResource.Resource.(type) {
			case mcp.TextResourceContents:
				uri = res.URI
			case mcp.BlobResourceContents:
				uri = res.URI
			}
			fmt.Printf("[Resource: %s]\n", uri)
		}
	}
}
