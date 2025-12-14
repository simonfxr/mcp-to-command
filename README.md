# mcp-to-command

Turn any MCP server into a CLI tool. Automatically generates commands, flags, and help text from the server's tool definitions.

## Installation

```bash
go install github.com/simonfxr/mcp-to-command@latest
```

Or build from source:

```bash
go build -o mcp-to-command .
```

## Usage

```
mcp-to-command <server-cmd> [server-args...] -- <tool-name> [--flag=value...]
```

The `--` separator divides MCP server arguments from tool arguments.

### List Available Tools

```bash
mcp-to-command go-jenkins-mcp -url example.com -auth user:pw -- --help
```

```
Available tools:

  jenkins_get_build_log_tail      Get the tail of build logs for a specific...
  jenkins_get_build_logs          Get build logs for a specific Jenkins job...
  jenkins_get_job                 Get detailed information about a specific...
  jenkins_get_jobs                Get list of Jenkins jobs with their current status
  jenkins_get_running_builds      Get list of currently running Jenkins builds
  jenkins_start_job               Trigger a Jenkins job build with optional parameters
  jenkins_wait_for_running_build  Wait for a running Jenkins build to complete

Use <tool-name> --help for more information about a tool.
```

### Get Tool Help

```bash
mcp-to-command go-jenkins-mcp -url example.com -auth user:pw -- jenkins_get_build_log_tail --help
```

```
jenkins_get_build_log_tail

  Get the tail of build logs for a specific Jenkins job and build number

Flags:
  --build_number <integer> (required)
      Build number
  --job_name <string> (required)
      Name of the Jenkins job
  --max_length <integer> (default: 8192)
      Maximum bytes from end of log to retrieve
```

### Execute Tools

```bash
# Tool with no parameters
mcp-to-command go-jenkins-mcp -url example.com -auth user:pw -- jenkins_get_jobs

# Tool with required parameters
mcp-to-command go-jenkins-mcp -url example.com -auth user:pw -- jenkins_get_job --name=my-pipeline

# Tool with optional parameters
mcp-to-command go-jenkins-mcp -url example.com -auth user:pw -- jenkins_get_build_log_tail \
  --job_name=my-pipeline \
  --build_number=123 \
  --max_length=16384

# Tool with JSON object parameter
mcp-to-command go-jenkins-mcp -url example.com -auth user:pw -- jenkins_start_job \
  --job_name=my-pipeline \
  --parameters='{"BRANCH":"feature/foo","DEPLOY":"true"}'
```

## How It Works

1. Starts the MCP server as a subprocess
2. Initializes the MCP connection and fetches the tool list
3. Generates CLI flags from each tool's JSON Schema (`inputSchema`)
4. Parses user-provided flags and converts types
5. Calls the requested tool and outputs the result as JSON

### Type Mapping

| JSON Schema Type | CLI Input                                  | Example                            |
|------------------|--------------------------------------------|------------------------------------|
| `string`         | `--flag=value`                             | `--name=my-job`                    |
| `integer`        | `--flag=123`                               | `--build_number=42`                |
| `number`         | `--flag=3.14`                              | `--threshold=0.5`                  |
| `boolean`        | `--flag`, `--no-flag`, `--flag=true/false` | `--verbose`, `--no-dry-run`        |
| `object`         | `--flag='{"k":"v"}'`                       | `--parameters='{"BRANCH":"main"}'` |
| `array`          | `--flag=item1 --flag=item2`                | `--tags=prod --tags=api`           |

### Flag Parsing Details

**Boolean Flags:**
- `--flag` sets the boolean property to `true`.
- `--no-flag` sets the boolean property to `false`.
- `--flag=true` and `--flag=false` are also supported.

**Array Flags:**
- You can provide values for `array` types by repeating the flag.
- Example: `--users=alice --users=bob` becomes `["alice", "bob"]`.
- The items are parsed according to the array's `items` type definition (e.g., array of integers).

**Object/JSON Flags:**
- Complex objects or arrays can also be passed as a single JSON string.

### Required vs Optional

- Properties in the schema's `required` array must be provided
- Properties with `default` values are optional
- Missing required flags show an error with the tool's help

## License

MIT
