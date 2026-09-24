// Package mcpserver exposes a running Bufflehead to MCP clients as a set of
// read-only data tools: list connections, read schemas, run SQL, cancel,
// fetch S3 objects, reconnect. It never drives the window.
//
// The tools are defined once against the Backend interface. Two things satisfy
// it: *control.Server (in-process, mounted at /mcp on the control server) and
// *control.Client (the stdio bridge in cmd/bufflehead-mcp, talking REST). The
// package must stay free of internal/db and internal/ui so the bridge can be
// built with CGO_ENABLED=0.
package mcpserver

import (
	"context"
	"net/http"

	"bufflehead/internal/control"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Backend is what the tools need from Bufflehead. *control.Server and
// *control.Client both implement it.
type Backend interface {
	Connections(ctx context.Context, includeColumns bool) ([]control.ConnectionInfo, error)
	ExecSQL(ctx context.Context, req control.SQLRequest) (*control.SQLResult, error)
	CancelSQL(ctx context.Context, conn string) error
	GetS3Object(ctx context.Context, req control.S3GetObjectRequest) (*control.S3GetObjectResult, error)
	Reconnect(ctx context.Context, conn string) (*control.ReconnectResult, error)
}

// New builds the MCP server with every tool registered against b.
func New(b Backend, version string) *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "bufflehead", Title: "Bufflehead", Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	registerTools(srv, b)
	return srv
}

// NewHTTPHandler serves srv over Streamable HTTP. Sessions are stateless: each
// request is self-contained, which suits a single-user localhost server and
// lets the bridge reconnect freely after the app restarts.
func NewHTTPHandler(srv *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}
