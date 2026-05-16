package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/juanatsap/drolo-mcp-docs/internal/pdf"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Determine documents directory
	docsDir := os.Getenv("DROLO_DOCS_DIR")
	if docsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("cannot determine home directory: %v", err)
		}
		docsDir = filepath.Join(home, ".drolo", "documents")
	}

	// Expand ~ if present
	if strings.HasPrefix(docsDir, "~/") {
		home, _ := os.UserHomeDir()
		docsDir = filepath.Join(home, docsDir[2:])
	}

	// Ensure documents directory exists
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		log.Printf("WARNING: documents directory does not exist: %s", docsDir)
		if err := os.MkdirAll(docsDir, 0755); err != nil {
			log.Fatalf("cannot create documents directory: %v", err)
		}
		log.Printf("Created documents directory: %s", docsDir)
	}

	// Initialize PDF reader
	reader := pdf.NewReader(docsDir)

	// Validate dependencies
	if err := reader.CheckDependencies(); err != nil {
		log.Printf("WARNING: %v", err)
	}

	// Create MCP server
	s := server.NewMCPServer(
		"drolo-docs",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register tools
	registerListDocuments(s, reader)
	registerReadDocument(s, reader)
	registerSearchDocument(s, reader)
	registerGetDocumentSummary(s, reader)

	// Run stdio transport
	stdio := server.NewStdioServer(s)
	if err := stdio.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func registerListDocuments(s *server.MCPServer, reader *pdf.Reader) {
	tool := mcp.NewTool("list_documents",
		mcp.WithDescription("List all available PDF documents with metadata (filename, title, pages, size)"),
	)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		docs, err := reader.ListDocuments()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error listing documents: %v", err)), nil
		}

		data, err := json.MarshalIndent(docs, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error marshaling results: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	})
}

func registerReadDocument(s *server.MCPServer, reader *pdf.Reader) {
	tool := mcp.NewTool("read_document",
		mcp.WithDescription("Read the text content of a PDF document. Optionally specify a page number."),
		mcp.WithString("filename",
			mcp.Required(),
			mcp.Description("The PDF filename to read"),
		),
		mcp.WithNumber("page",
			mcp.Description("Optional page number to read (1-based). If omitted, returns full text."),
		),
	)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename, err := request.RequireString("filename")
		if err != nil {
			return mcp.NewToolResultError("filename parameter is required"), nil
		}

		page := request.GetInt("page", 0)

		text, err := reader.ReadDocument(filename, page)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error reading document: %v", err)), nil
		}

		return mcp.NewToolResultText(text), nil
	})
}

func registerSearchDocument(s *server.MCPServer, reader *pdf.Reader) {
	tool := mcp.NewTool("search_document",
		mcp.WithDescription("Search for text within a PDF document. Returns matching lines with context and approximate page numbers."),
		mcp.WithString("filename",
			mcp.Required(),
			mcp.Description("The PDF filename to search"),
		),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The search query (case-insensitive)"),
		),
	)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename, err := request.RequireString("filename")
		if err != nil {
			return mcp.NewToolResultError("filename parameter is required"), nil
		}

		query, err := request.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError("query parameter is required"), nil
		}

		result, err := reader.SearchDocument(filename, query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error searching document: %v", err)), nil
		}

		return mcp.NewToolResultText(result), nil
	})
}

func registerGetDocumentSummary(s *server.MCPServer, reader *pdf.Reader) {
	tool := mcp.NewTool("get_document_summary",
		mcp.WithDescription("Get a summary of a PDF document (first 3 pages of text)."),
		mcp.WithString("filename",
			mcp.Required(),
			mcp.Description("The PDF filename to summarize"),
		),
	)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename, err := request.RequireString("filename")
		if err != nil {
			return mcp.NewToolResultError("filename parameter is required"), nil
		}

		text, err := reader.GetDocumentSummary(filename)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error getting summary: %v", err)), nil
		}

		return mcp.NewToolResultText(text), nil
	})
}
