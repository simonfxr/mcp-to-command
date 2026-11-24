# mcp-to-command

Turn any MCP server into a CLI tool. Automatically generates commands, flags, and help text from the server's tool definitions.

## Installation

```bash
go install github.com/simonfxr/mcp-to-command@latest
```

With Nix:

```bash
nix run github:simonfxr/mcp-to-command -- <server-cmd> [server-args...] -- <tool-name> ...
nix profile install github:simonfxr/mcp-to-command
```

Or build from source:

```bash
go build -o mcp-to-command .
```

## Usage

```
mcp-to-command <server-cmd> [server-args...] -- <tool-name> [--flag value | --flag=value ...]
mcp-to-command <http(s)://server-url> -- <tool-name> [--flag value | --flag=value ...]
```

The `--` separator divides MCP server arguments from tool arguments.

Local servers are started as a subprocess and spoken to over stdio. Remote
servers are reached via the Streamable HTTP transport; URLs whose path ends in
`/sse` use the legacy SSE transport instead.

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

1. Starts the MCP server as a subprocess (or connects to it over HTTP)
2. Initializes the MCP connection and fetches the tool list
3. Generates CLI flags from each tool's JSON Schema (`inputSchema`)
4. Parses user-provided flags and converts types
5. Calls the requested tool and prints the result (JSON text is pretty-printed);
   tool errors are written to stderr with a non-zero exit code

### Tool Output

Stdout only ever receives text. Binary content is saved to a file and
referenced by its path:

| Content                  | Output                                                    |
|--------------------------|-----------------------------------------------------------|
| Text                     | printed as is, JSON is pretty-printed                     |
| Image / audio            | `[Image: image/png, 48213 bytes] /tmp/mcp-to-command-1234.png` |
| Embedded text resource   | `[Resource: <uri> (<mime>)]` followed by the text         |
| Embedded binary resource | `[Resource: <uri> (<mime>, 512 bytes)] /tmp/mcp-to-command-5678.pdf` |
| Resource link            | read from the server and printed like an embedded resource |

**Saved files:**
- Files are written to the system temp directory, or to the directory named by
  `MCP_TO_COMMAND_OUTPUT_DIR` (created if missing).
- They are never deleted by `mcp-to-command`; cleaning up is up to the caller.
- The extension is derived from the MIME type, then from the resource URI, and
  is `.bin` otherwise.
- If a payload cannot be decoded or saved, a warning goes to stderr and a
  placeholder such as `[Image: image/png, 64284 bytes base64]` is printed.

**Resource links:**
- Links are resolved with `resources/read` while the connection is still open,
  if the server advertises the `resources` capability.
- If that is not possible, the link itself is printed, with a warning on stderr
  if the read failed:
  `[Resource link: <uri> (<title>, <mime>, <size> bytes)]` followed by its
  description.

### Type Mapping

| JSON Schema Type | CLI Input                                  | Example                            |
|------------------|--------------------------------------------|------------------------------------|
| `string`         | `--flag value` or `--flag=value`           | `--name my-job`                    |
| `integer`        | `--flag 123` or `--flag=123`               | `--build_number 42`                |
| `number`         | `--flag 3.14` or `--flag=3.14`             | `--threshold 0.5`                  |
| `boolean`        | `--flag`, `--no-flag`, `--flag=true/false` | `--verbose`, `--no-dry-run`        |
| `object`         | `--flag '{"k":"v"}'`                       | `--parameters '{"BRANCH":"main"}'` |
| `array`          | `--flag a --flag b` (repeatable)           | `--tags prod --tags api`           |

### Flag Parsing Details

**Boolean Flags:**
- `--flag` sets the boolean property to `true`.
- `--no-flag` sets the boolean property to `false`.
- `--flag=true` and `--flag=false` are also supported.

**Array Flags:**
- Provide values for `array` types by repeating the flag.
- Example: `--users alice --users bob` becomes `["alice", "bob"]`.
- The items are parsed according to the array's `items` type definition (e.g., array of integers).

**Object/JSON Flags:**
- `object` properties (and array items of type `object`) are passed as a JSON string.

**Nullable Types:**
- Union types such as `["string", "null"]` use the first non-null type.

### Required vs Optional

- Properties in the schema's `required` array must be provided
- Properties with `default` values are optional
- Missing required flags show an error with the tool's help

## License

MIT
