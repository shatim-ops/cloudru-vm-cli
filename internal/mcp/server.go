// Package mcp implements a Model Context Protocol (MCP) server that exposes
// cloudru-vm operations as tools for AI agents. It speaks JSON-RPC 2.0 over
// stdio (newline-delimited JSON), the MCP stdio transport.
//
// IMPORTANT: stdout is reserved for the JSON-RPC channel. Nothing else may be
// written to it or the protocol stream is corrupted. All engine output is
// redirected to in-memory buffers (see tools.go); diagnostics go to stderr.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
)

// defaultProtocolVersion is the MCP protocol version advertised when the client
// does not request a specific one. It is a widely supported baseline.
const defaultProtocolVersion = "2024-11-05"

// serverName is the MCP server identity reported during initialize.
const serverName = "cloudru-vm"

// EngineFactory builds an *engine.Engine from a set of config overrides.
// Overrides use the same field names as config flags (e.g. "ComposeFile",
// "Region", "ProjectID"). A nil or empty map yields the default config.
type EngineFactory func(overrides map[string]any) (*engine.Engine, error)

// toolHandler executes a single tool call. It returns the textual result and a
// flag indicating whether the result represents a tool-level error (reported to
// the model via the MCP isError field, not as a JSON-RPC protocol error).
type toolHandler func(ctx context.Context, args map[string]any) (text string, isError bool)

// Tool is an MCP tool definition advertised via tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Server is an MCP server exposing cloudru-vm operations over stdio.
type Server struct {
	newEngine EngineFactory
	version   string
	tools     []Tool
	handlers  map[string]toolHandler
}

// New creates an MCP server. factory builds engines per tool call (so each call
// reflects the current working directory, config, and SSH keys); version is the
// CLI version reported during initialize.
func New(factory EngineFactory, version string) *Server {
	s := &Server{
		newEngine: factory,
		version:   version,
	}
	s.registerTools()
	return s
}

// ListTools returns the advertised tool definitions.
func (s *Server) ListTools() []Tool {
	return s.tools
}

// --- JSON-RPC 2.0 wire types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC standard error codes.
const (
	codeParseError     = -32700
	codeInvalidReq     = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Run serves the JSON-RPC loop over the given reader/writer until EOF.
// Messages are newline-delimited JSON objects (MCP stdio transport).
func (s *Server) Run(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Allow large request lines (tool arguments may be sizeable).
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	enc := json.NewEncoder(out)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(&rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: codeParseError, Message: "parse error"},
			})
			continue
		}

		resp := s.handle(&req)
		if resp != nil {
			if err := enc.Encode(resp); err != nil {
				return fmt.Errorf("write response: %w", err)
			}
		}
	}

	return scanner.Err()
}

// isNotification reports whether the request lacks an id (JSON-RPC notification,
// which must not receive a response).
func (r *rpcRequest) isNotification() bool {
	return len(r.ID) == 0
}

// handle dispatches a single request. It returns nil for notifications.
func (s *Server) handle(req *rpcRequest) *rpcResponse {
	switch req.Method {
	case "initialize":
		return s.ok(req, s.initializeResult(req.Params))

	case "notifications/initialized", "initialized":
		// Client acknowledgement; no response.
		return nil

	case "ping":
		return s.ok(req, map[string]any{})

	case "tools/list":
		return s.ok(req, map[string]any{"tools": s.tools})

	case "tools/call":
		return s.handleToolCall(req)

	default:
		if req.isNotification() {
			return nil
		}
		return s.fail(req, codeMethodNotFound, "method not found: "+req.Method)
	}
}

// initializeResult builds the initialize response, echoing the client's
// requested protocol version when present (and supported) for compatibility.
func (s *Server) initializeResult(params json.RawMessage) map[string]any {
	protocolVersion := defaultProtocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
			protocolVersion = p.ProtocolVersion
		}
	}

	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": s.version,
		},
	}
}

// handleToolCall dispatches a tools/call request to the matching handler.
func (s *Server) handleToolCall(req *rpcRequest) *rpcResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.fail(req, codeInvalidParams, "invalid tool call params: "+err.Error())
		}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}

	handler, ok := s.handlers[p.Name]
	if !ok {
		// Unknown tool is surfaced to the model as a tool error, not a
		// protocol error, so it can recover.
		return s.toolResult(req, "unknown tool: "+p.Name, true)
	}

	text, isErr := handler(context.Background(), p.Arguments)
	return s.toolResult(req, text, isErr)
}

// toolResult wraps a tool's textual output in the MCP tools/call result shape.
func (s *Server) toolResult(req *rpcRequest, text string, isError bool) *rpcResponse {
	return s.ok(req, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	})
}

// ok builds a success response carrying result.
func (s *Server) ok(req *rpcRequest, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// fail builds a JSON-RPC protocol error response.
func (s *Server) fail(req *rpcRequest, code int, msg string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
}
