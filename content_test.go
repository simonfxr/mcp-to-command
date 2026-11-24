package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// testServerEnv makes the test binary act as a stdio MCP server, so that
// run() can be tested end to end against a real subprocess.
const testServerEnv = "MCP_TO_COMMAND_TEST_SERVER"

var (
	testImage = []byte{0x89, 'P', 'N', 'G', 0xff, 0xfe, 0x00}
	testAudio = []byte("RIFF\x00\x01WAVE")
	testBlob  = []byte{0x00, 0x01, 0x02, 0xfb, 0xff}
)

func b64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func TestMain(m *testing.M) {
	if os.Getenv(testServerEnv) != "" {
		if err := server.ServeStdio(newTestServer()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func newTestServer() *server.MCPServer {
	s := server.NewMCPServer("test", "0", server.WithResourceCapabilities(false, false))

	s.AddResource(mcp.NewResource("test://report", "report"),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "text/plain", Text: "report body"},
			}, nil
		})
	s.AddResource(mcp.NewResource("test://files/dump.dat", "dump"),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.BlobResourceContents{URI: req.Params.URI, Blob: b64(testBlob)},
			}, nil
		})

	s.AddTool(mcp.NewTool("media"),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{
				mcp.NewTextContent("hello"),
				mcp.NewImageContent(b64(testImage), "image/png"),
				mcp.NewAudioContent(b64(testAudio), "audio/wav"),
				mcp.NewEmbeddedResource(mcp.BlobResourceContents{
					URI: "test://embedded", MIMEType: "application/pdf", Blob: b64(testBlob),
				}),
				mcp.NewResourceLink("test://report", "report", "", "text/plain"),
				mcp.NewResourceLink("test://files/dump.dat", "dump", "", ""),
				mcp.NewResourceLink("test://missing", "missing", "Not served", "text/plain"),
			}}, nil
		})
	s.AddTool(mcp.NewTool("fail"),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result := mcp.NewToolResultError("it broke")
			result.Content = append(result.Content, mcp.NewImageContent(b64(testImage), "image/png"))
			return result, nil
		})
	return s
}

// capture redirects *target (os.Stdout or os.Stderr) to a file while fn runs.
func capture(t *testing.T, target **os.File, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "capture")
	if err != nil {
		t.Fatal(err)
	}
	old := *target
	defer func() {
		*target = old
		f.Close()
	}()
	*target = f
	fn()
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// useOutputDir points binary output at a not yet existing directory.
func useOutputDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "nested", "out")
	t.Setenv(outputDirEnv, dir)
	return dir
}

// checkSaved asserts that line is "<prefix> <path>", with path being a file
// in dir that has the given extension and contents.
func checkSaved(t *testing.T, line, prefix, dir, ext string, want []byte) {
	t.Helper()
	file, ok := strings.CutPrefix(line, prefix+" ")
	if !ok {
		t.Errorf("line %q does not start with %q", line, prefix+" ")
		return
	}
	if filepath.Dir(file) != dir || filepath.Ext(file) != ext {
		t.Errorf("file %q is not a %s file in %s", file, ext, dir)
	}
	if !strings.HasPrefix(filepath.Base(file), progName+"-") {
		t.Errorf("file %q is not prefixed with %q", file, progName+"-")
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Error(err)
		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("file %q contains %q, want %q", file, got, want)
	}
}

func splitLines(t *testing.T, out string, want int) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != want {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), want, out)
	}
	return lines
}

func TestFileExtension(t *testing.T) {
	for _, tt := range []struct {
		mimeType, uri, want string
	}{
		{"image/png", "", ".png"},
		{"image/jpeg", "", ".jpg"},
		{"IMAGE/JPEG; charset=binary", "", ".jpg"},
		{"image/png", "test://x/picture.gif", ".png"},
		{"application/wasm", "", ".wasm"},
		{"application/octet-stream", "file:///a/b/archive.ZST?x=1", ".zst"},
		{"application/x-does-not-exist", "test://x/y.dat", ".dat"},
		{"application/x-does-not-exist", "", ".bin"},
		{"", "test://x/data.tar.gz", ".gz"},
		{"", "test://x/no-extension", ".bin"},
		{"", "test://x/odd.a-b", ".bin"},
		{"", "test://x/long.abcdefghijkl", ".bin"},
		{"", "test://x/trailing.", ".bin"},
		{"", "", ".bin"},
	} {
		if got := fileExtension(tt.mimeType, tt.uri); got != tt.want {
			t.Errorf("fileExtension(%q, %q) = %q, want %q", tt.mimeType, tt.uri, got, tt.want)
		}
	}
}

func TestDecodeBase64(t *testing.T) {
	want := []byte{0xfb, 0xff, 0xfe, 0x01}
	for _, input := range []string{
		base64.StdEncoding.EncodeToString(want),
		base64.RawStdEncoding.EncodeToString(want),
		base64.URLEncoding.EncodeToString(want),
		base64.RawURLEncoding.EncodeToString(want),
		" +//+\nAQ==\n",
	} {
		got, err := decodeBase64(input)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("decodeBase64(%q) = %v, %v, want %v", input, got, err, want)
		}
	}
	if _, err := decodeBase64("not base64!"); err == nil {
		t.Error("decodeBase64 should reject invalid input")
	}
}

func TestPrintBinaryContent(t *testing.T) {
	dir := useOutputDir(t)
	var out, warn bytes.Buffer
	p := resultPrinter{out: &out, warn: &warn}
	p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{
		mcp.NewImageContent(b64(testImage), "image/png"),
		mcp.NewAudioContent(b64(testAudio), "audio/wav"),
		mcp.NewEmbeddedResource(mcp.BlobResourceContents{
			URI: "test://doc", MIMEType: "application/pdf", Blob: b64(testBlob),
		}),
		mcp.NewEmbeddedResource(mcp.BlobResourceContents{URI: "test://files/raw.dat", Blob: b64(testBlob)}),
	}})

	lines := splitLines(t, out.String(), 4)
	checkSaved(t, lines[0], "[Image: image/png, 7 bytes]", dir, ".png", testImage)
	checkSaved(t, lines[1], "[Audio: audio/wav, 10 bytes]", dir, ".wav", testAudio)
	checkSaved(t, lines[2], "[Resource: test://doc (application/pdf, 5 bytes)]", dir, ".pdf", testBlob)
	checkSaved(t, lines[3], "[Resource: test://files/raw.dat (5 bytes)]", dir, ".dat", testBlob)
	if warn.Len() != 0 {
		t.Errorf("unexpected warnings: %s", warn.String())
	}
}

func TestPrintBinaryContentDefaultDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv(outputDirEnv, "")
	var out, warn bytes.Buffer
	p := resultPrinter{out: &out, warn: &warn}
	p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{
		mcp.NewImageContent(b64(testImage), "image/png"),
	}})
	checkSaved(t, splitLines(t, out.String(), 1)[0], "[Image: image/png, 7 bytes]", tmp, ".png", testImage)
}

func TestPrintUnsavableContent(t *testing.T) {
	content := []mcp.Content{
		mcp.NewImageContent("!!!", "image/png"),
		mcp.NewEmbeddedResource(mcp.BlobResourceContents{
			URI: "test://doc", MIMEType: "application/pdf", Blob: "!!!",
		}),
	}
	wantOut := "[Image: image/png, 3 bytes base64]\n" +
		"[Resource: test://doc (application/pdf, 3 bytes base64)]\n"

	t.Run("invalid base64", func(t *testing.T) {
		dir := useOutputDir(t)
		var out, warn bytes.Buffer
		p := resultPrinter{out: &out, warn: &warn}
		p.print(t.Context(), &mcp.CallToolResult{Content: content})

		if out.String() != wantOut {
			t.Errorf("out = %q, want %q", out.String(), wantOut)
		}
		for _, want := range []string{
			"Warning: could not save image: decoding base64:",
			"Warning: could not save resource test://doc: decoding base64:",
		} {
			if !strings.Contains(warn.String(), want) {
				t.Errorf("warnings %q do not contain %q", warn.String(), want)
			}
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("unexpected files in %s: %v", dir, entries)
		}
	})

	t.Run("unwritable directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(outputDirEnv, filepath.Join(file, "sub"))
		var out, warn bytes.Buffer
		p := resultPrinter{out: &out, warn: &warn}
		p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{
			mcp.NewImageContent(b64(testImage), "image/png"),
			mcp.NewTextContent("still printed"),
		}})

		want := fmt.Sprintf("[Image: image/png, %d bytes base64]\nstill printed\n", len(b64(testImage)))
		if out.String() != want {
			t.Errorf("out = %q, want %q", out.String(), want)
		}
		if !strings.HasPrefix(warn.String(), "Warning: could not save image: ") {
			t.Errorf("unexpected warnings: %q", warn.String())
		}
	})
}

func TestPrintTextResource(t *testing.T) {
	var out, warn bytes.Buffer
	p := resultPrinter{out: &out, warn: &warn}
	p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{
		mcp.NewEmbeddedResource(mcp.TextResourceContents{URI: "test://a", MIMEType: "text/markdown", Text: "# A"}),
		mcp.NewEmbeddedResource(mcp.TextResourceContents{URI: "test://b", Text: "B"}),
	}})
	want := "[Resource: test://a (text/markdown)]\n# A\n[Resource: test://b]\nB\n"
	if out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}
}

type fakeReader struct {
	contents map[string][]mcp.ResourceContents
	read     []string
}

func (f *fakeReader) ReadResource(_ context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	f.read = append(f.read, req.Params.URI)
	contents, ok := f.contents[req.Params.URI]
	if !ok {
		return nil, errors.New("resource not found")
	}
	return &mcp.ReadResourceResult{Contents: contents}, nil
}

func TestPrintResourceLink(t *testing.T) {
	size := int64(42)
	link := mcp.NewResourceLink("test://missing", "name", "A description", "text/plain")
	link.Title = "Title"
	link.Size = &size
	wantLink := "[Resource link: test://missing (Title, text/plain, 42 bytes)]\nA description\n"

	t.Run("fetched", func(t *testing.T) {
		dir := useOutputDir(t)
		reader := &fakeReader{contents: map[string][]mcp.ResourceContents{
			"test://text": {mcp.TextResourceContents{URI: "test://text", Text: "body"}},
			"test://blob": {mcp.BlobResourceContents{URI: "test://blob", Blob: b64(testBlob)}},
		}}
		var out, warn bytes.Buffer
		p := resultPrinter{out: &out, warn: &warn, resources: reader}
		p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{
			mcp.NewResourceLink("test://text", "text", "ignored", "text/plain"),
			mcp.NewResourceLink("test://blob", "blob", "", "image/png"),
		}})

		lines := splitLines(t, out.String(), 3)
		// The link's MIME type fills in for the one missing on the contents.
		if got := lines[0] + "\n" + lines[1]; got != "[Resource: test://text (text/plain)]\nbody" {
			t.Errorf("text resource printed as %q", got)
		}
		checkSaved(t, lines[2], "[Resource: test://blob (image/png, 5 bytes)]", dir, ".png", testBlob)
		if warn.Len() != 0 {
			t.Errorf("unexpected warnings: %s", warn.String())
		}
	})

	t.Run("read fails", func(t *testing.T) {
		reader := &fakeReader{}
		var out, warn bytes.Buffer
		p := resultPrinter{out: &out, warn: &warn, resources: reader}
		p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{link}})

		if out.String() != wantLink {
			t.Errorf("out = %q, want %q", out.String(), wantLink)
		}
		if want := "Warning: could not read resource test://missing: resource not found\n"; warn.String() != want {
			t.Errorf("warn = %q, want %q", warn.String(), want)
		}
	})

	t.Run("empty contents", func(t *testing.T) {
		reader := &fakeReader{contents: map[string][]mcp.ResourceContents{"test://missing": nil}}
		var out, warn bytes.Buffer
		p := resultPrinter{out: &out, warn: &warn, resources: reader}
		p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{link}})

		if out.String() != wantLink || warn.Len() != 0 {
			t.Errorf("out = %q, warn = %q, want %q and no warning", out.String(), warn.String(), wantLink)
		}
	})

	t.Run("no reader", func(t *testing.T) {
		var out, warn bytes.Buffer
		p := resultPrinter{out: &out, warn: &warn}
		p.print(t.Context(), &mcp.CallToolResult{Content: []mcp.Content{
			mcp.NewResourceLink("test://bare", "", "", ""),
		}})

		if want := "[Resource link: test://bare]\n"; out.String() != want || warn.Len() != 0 {
			t.Errorf("out = %q, warn = %q, want %q and no warning", out.String(), warn.String(), want)
		}
	})
}

// runCaptured runs the CLI against the test binary acting as MCP server.
func runCaptured(t *testing.T, toolArgs ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv(testServerEnv, "1")
	stderr = capture(t, &os.Stderr, func() {
		stdout = capture(t, &os.Stdout, func() {
			code = run(append([]string{os.Args[0], "--"}, toolArgs...))
		})
	})
	return code, stdout, stderr
}

func TestRunBinaryContent(t *testing.T) {
	dir := useOutputDir(t)
	code, stdout, stderr := runCaptured(t, "media")
	if code != 0 {
		t.Fatalf("run() = %d, stderr:\n%s", code, stderr)
	}

	lines := splitLines(t, stdout, 9)
	if lines[0] != "hello" {
		t.Errorf("text printed as %q", lines[0])
	}
	checkSaved(t, lines[1], "[Image: image/png, 7 bytes]", dir, ".png", testImage)
	checkSaved(t, lines[2], "[Audio: audio/wav, 10 bytes]", dir, ".wav", testAudio)
	checkSaved(t, lines[3], "[Resource: test://embedded (application/pdf, 5 bytes)]", dir, ".pdf", testBlob)
	if got := lines[4] + "\n" + lines[5]; got != "[Resource: test://report (text/plain)]\nreport body" {
		t.Errorf("linked text resource printed as %q", got)
	}
	checkSaved(t, lines[6], "[Resource: test://files/dump.dat (5 bytes)]", dir, ".dat", testBlob)
	if got := lines[7] + "\n" + lines[8]; got != "[Resource link: test://missing (missing, text/plain)]\nNot served" {
		t.Errorf("unreadable link printed as %q", got)
	}
	if !strings.Contains(stderr, "Warning: could not read resource test://missing: ") {
		t.Errorf("stderr %q lacks the warning for the unreadable link", stderr)
	}
}

func TestRunErrorResult(t *testing.T) {
	dir := useOutputDir(t)
	code, stdout, stderr := runCaptured(t, "fail")
	if code != 1 {
		t.Errorf("run() = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want it empty", stdout)
	}
	lines := splitLines(t, stderr, 2)
	if lines[0] != "it broke" {
		t.Errorf("error text printed as %q", lines[0])
	}
	checkSaved(t, lines[1], "[Image: image/png, 7 bytes]", dir, ".png", testImage)
}
