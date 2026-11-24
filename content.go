package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// outputDirEnv names the environment variable that overrides the directory
// binary tool output is saved to. It defaults to the system temp directory.
const outputDirEnv = "MCP_TO_COMMAND_OUTPUT_DIR"

// mimeExtensions maps common MIME types to their conventional extension.
// mime.ExtensionsByType alone is not enough: it returns extensions sorted
// alphabetically, so image/jpeg would become ".jfif".
var mimeExtensions = map[string]string{
	"application/gzip": ".gz",
	"application/json": ".json",
	"application/pdf":  ".pdf",
	"application/zip":  ".zip",
	"audio/aac":        ".aac",
	"audio/flac":       ".flac",
	"audio/mp4":        ".m4a",
	"audio/mpeg":       ".mp3",
	"audio/ogg":        ".ogg",
	"audio/wav":        ".wav",
	"audio/webm":       ".weba",
	"audio/x-wav":      ".wav",
	"image/avif":       ".avif",
	"image/bmp":        ".bmp",
	"image/gif":        ".gif",
	"image/jpeg":       ".jpg",
	"image/png":        ".png",
	"image/svg+xml":    ".svg",
	"image/tiff":       ".tiff",
	"image/webp":       ".webp",
	"text/csv":         ".csv",
	"text/html":        ".html",
	"text/plain":       ".txt",
	"video/mp4":        ".mp4",
	"video/webm":       ".webm",
}

// fileExtension picks a file extension for a payload from its MIME type,
// falling back to the extension of its URI and finally to ".bin".
func fileExtension(mimeType, uri string) string {
	// application/octet-stream says nothing about the format, so it is
	// treated like a missing MIME type.
	if mt, _, err := mime.ParseMediaType(mimeType); err == nil && mt != "application/octet-stream" {
		if ext, ok := mimeExtensions[mt]; ok {
			return ext
		}
		if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
			return exts[0]
		}
	}
	if u, err := url.Parse(uri); err == nil {
		if ext := strings.ToLower(path.Ext(u.Path)); isPlainExtension(ext) {
			return ext
		}
	}
	return ".bin"
}

// isPlainExtension reports whether ext is a short alphanumeric extension
// such as ".png", so that arbitrary URI suffixes do not end up in file names.
func isPlainExtension(ext string) bool {
	if len(ext) < 2 || len(ext) > 9 || ext[0] != '.' {
		return false
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// decodeBase64 decodes standard or URL-safe base64, with or without padding.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if strings.ContainsAny(s, "-_") {
		return base64.RawURLEncoding.DecodeString(s)
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// saveBinary decodes a base64 payload into a new file and returns its
// absolute path and decoded size. The file is left in place for the caller
// of the CLI to consume; nothing cleans it up.
func saveBinary(encoded, mimeType, uri string) (string, int, error) {
	data, err := decodeBase64(encoded)
	if err != nil {
		return "", 0, fmt.Errorf("decoding base64: %w", err)
	}

	dir := os.Getenv(outputDirEnv)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", 0, err
		}
	}
	f, err := os.CreateTemp(dir, progName+"-*"+fileExtension(mimeType, uri))
	if err != nil {
		return "", 0, err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", 0, err
	}

	name, err := filepath.Abs(f.Name())
	if err != nil {
		name = f.Name()
	}
	return name, len(data), nil
}

// tag formats "[kind: subject (detail, ...)]", leaving out empty details.
func tag(kind, subject string, details ...string) string {
	details = slices.DeleteFunc(details, func(s string) bool { return s == "" })
	if len(details) == 0 {
		return fmt.Sprintf("[%s: %s]", kind, subject)
	}
	return fmt.Sprintf("[%s: %s (%s)]", kind, subject, strings.Join(details, ", "))
}

// resourceReader fetches the contents of a resource by URI. It is
// implemented by *client.Client.
type resourceReader interface {
	ReadResource(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
}

// resultPrinter renders tool results as text. Binary payloads are saved to
// files and referenced by path, so out only ever receives text.
type resultPrinter struct {
	out  io.Writer
	warn io.Writer
	// resources resolves resource links. If nil, links are printed as is.
	resources resourceReader
}

func (p *resultPrinter) print(ctx context.Context, result *mcp.CallToolResult) {
	if len(result.Content) == 0 && result.StructuredContent != nil {
		printJSON(p.out, result.StructuredContent)
		return
	}

	for _, content := range result.Content {
		switch c := content.(type) {
		case mcp.TextContent:
			// Pretty-print JSON payloads, pass everything else through.
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, []byte(strings.TrimSpace(c.Text)), "", "  "); err == nil {
				fmt.Fprintln(p.out, pretty.String())
			} else {
				fmt.Fprintln(p.out, c.Text)
			}
		case mcp.ImageContent:
			p.printMedia("Image", c.MIMEType, c.Data)
		case mcp.AudioContent:
			p.printMedia("Audio", c.MIMEType, c.Data)
		case mcp.ResourceLink:
			p.printLink(ctx, c)
		case mcp.EmbeddedResource:
			p.printResource(c.Resource, "")
		}
	}
}

// printMedia saves an image or audio payload and prints a reference to it.
func (p *resultPrinter) printMedia(kind, mimeType, data string) {
	file, size, err := saveBinary(data, mimeType, "")
	if err != nil {
		fmt.Fprintf(p.warn, "Warning: could not save %s: %v\n", strings.ToLower(kind), err)
		fmt.Fprintf(p.out, "[%s: %s, %d bytes base64]\n", kind, mimeType, len(data))
		return
	}
	fmt.Fprintf(p.out, "[%s: %s, %d bytes] %s\n", kind, mimeType, size, file)
}

// printResource prints text resources inline and saves binary ones to a
// file. fallbackMIME is used if the resource does not carry a MIME type.
func (p *resultPrinter) printResource(res mcp.ResourceContents, fallbackMIME string) {
	switch r := res.(type) {
	case mcp.TextResourceContents:
		fmt.Fprintf(p.out, "%s\n%s\n", tag("Resource", r.URI, cmp.Or(r.MIMEType, fallbackMIME)), r.Text)
	case mcp.BlobResourceContents:
		mimeType := cmp.Or(r.MIMEType, fallbackMIME)
		file, size, err := saveBinary(r.Blob, mimeType, r.URI)
		if err != nil {
			fmt.Fprintf(p.warn, "Warning: could not save resource %s: %v\n", r.URI, err)
			fmt.Fprintln(p.out, tag("Resource", r.URI, mimeType, fmt.Sprintf("%d bytes base64", len(r.Blob))))
			return
		}
		fmt.Fprintf(p.out, "%s %s\n", tag("Resource", r.URI, mimeType, fmt.Sprintf("%d bytes", size)), file)
	}
}

// printLink reads the linked resource from the server and prints it like an
// embedded resource. The link is only resolvable while the session is open,
// so it is fetched now; if that fails the link itself is printed instead.
func (p *resultPrinter) printLink(ctx context.Context, link mcp.ResourceLink) {
	if p.resources != nil {
		req := mcp.ReadResourceRequest{}
		req.Params.URI = link.URI
		res, err := p.resources.ReadResource(ctx, req)
		switch {
		case err != nil:
			fmt.Fprintf(p.warn, "Warning: could not read resource %s: %v\n", link.URI, err)
		case len(res.Contents) > 0:
			for _, contents := range res.Contents {
				p.printResource(contents, link.MIMEType)
			}
			return
		}
	}

	size := ""
	if link.Size != nil {
		size = fmt.Sprintf("%d bytes", *link.Size)
	}
	fmt.Fprintln(p.out, tag("Resource link", link.URI, cmp.Or(link.Title, link.Name), link.MIMEType, size))
	if link.Description != "" {
		fmt.Fprintln(p.out, link.Description)
	}
}
