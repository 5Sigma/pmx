package mcpserver

import (
	"context"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailops"
	"github.com/5Sigma/pmx/internal/secrets"
)

// Server exposes pmx mail functionality as an MCP tool server over stdio.
type Server struct {
	st         mail.MessageStore
	ops        mailops.MailOps // nil → mutations write local DB only
	send       mailops.SendOps // nil → sends are queued but not triggered
	cfg        *config.Config
	sec        *secrets.Store
	logger     *slog.Logger
	attachsDir string
}

// New creates a Server. ops and send may be nil; when both are nil, mutations
// update the local SQLite store but do not propagate to the IMAP/SMTP backend.
func New(st mail.MessageStore, ops mailops.MailOps, send mailops.SendOps,
	cfg *config.Config, sec *secrets.Store, attachsDir string, logger *slog.Logger) *Server {
	return &Server{
		st:         st,
		ops:        ops,
		send:       send,
		cfg:        cfg,
		sec:        sec,
		attachsDir: attachsDir,
		logger:     logger,
	}
}

// Serve registers all tools and serves the MCP protocol over stdin/stdout.
// Returns when ctx is cancelled or stdin reaches EOF.
func (s *Server) Serve(ctx context.Context) error {
	srv := mcpsrv.NewMCPServer("pmx", "0.1.0")
	s.registerTools(srv)

	stdio := mcpsrv.NewStdioServer(srv)
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}

func (s *Server) registerTools(srv *mcpsrv.MCPServer) {
	srv.AddTool(mcp.NewTool("list_accounts",
		mcp.WithDescription("List all configured email accounts."),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleListAccounts)

	srv.AddTool(mcp.NewTool("list_folders",
		mcp.WithDescription("List folders for an account with unread and total message counts."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleListFolders)

	srv.AddTool(mcp.NewTool("get_message",
		mcp.WithDescription("Fetch a single message by UID, including headers, body, and attachment metadata."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Exact IMAP folder name (e.g. INBOX).")),
		mcp.WithString("role", mcp.Description("Semantic folder role: inbox|sent|drafts|archive|trash|junk.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID of the message.")),
		mcp.WithString("body", mcp.Description("Body format to include: text (default), html, both.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleGetMessage)

	srv.AddTool(mcp.NewTool("get_attachment",
		mcp.WithDescription("Download an attachment by ID. Returns base64 content (capped at 10 MB); larger files return the on-disk path instead."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Exact IMAP folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID of the message.")),
		mcp.WithInteger("attachment_id", mcp.Required(), mcp.Description("Attachment ID from get_message response.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleGetAttachment)

	srv.AddTool(mcp.NewTool("list_messages",
		mcp.WithDescription("List messages in a folder, newest first. Supports optional unread-only filter and pagination."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Exact IMAP folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role: inbox|sent|drafts|archive|trash|junk.")),
		mcp.WithBoolean("unread_only", mcp.Description("When true, return only unread messages (default false).")),
		mcp.WithInteger("limit", mcp.Description("Maximum results (default 25, max 200).")),
		mcp.WithInteger("offset", mcp.Description("Pagination offset (default 0).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleListMessages)

	srv.AddTool(mcp.NewTool("search_messages",
		mcp.WithDescription("Full-text search across all messages using SQLite FTS5 (searches subject, from, to, body). Optionally filter by account or folder."),
		mcp.WithString("query", mcp.Required(), mcp.Description("FTS5 search query.")),
		mcp.WithString("account", mcp.Description("Restrict to this account name.")),
		mcp.WithString("folder", mcp.Description("Restrict to this folder name.")),
		mcp.WithInteger("limit", mcp.Description("Maximum results (default 25, max 100).")),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleSearchMessages)

	srv.AddTool(mcp.NewTool("unread_count",
		mcp.WithDescription("Count unread messages. If folder is given, returns one number. If only account, sums unread across all non-trash/junk folders. If neither, returns a per-account map."),
		mcp.WithString("account", mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleUnreadCount)

	srv.AddTool(mcp.NewTool("message_count",
		mcp.WithDescription("Count messages stored locally in a folder."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithReadOnlyHintAnnotation(true),
	), s.handleMessageCount)

	srv.AddTool(mcp.NewTool("compose_message",
		mcp.WithDescription("Compose and send a new email message."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name to send from.")),
		mcp.WithArray("to", mcp.Required(), mcp.Description("Recipient addresses."),
			mcp.WithStringItems()),
		mcp.WithArray("cc", mcp.Description("CC addresses."), mcp.WithStringItems()),
		mcp.WithArray("bcc", mcp.Description("BCC addresses."), mcp.WithStringItems()),
		mcp.WithString("subject", mcp.Required(), mcp.Description("Email subject.")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Email body text.")),
		mcp.WithBoolean("markdown", mcp.Description("Render body as Markdown (default false).")),
		mcp.WithArray("attachments", mcp.Description("File attachments."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"filename":  map[string]any{"type": "string", "description": "File name."},
					"mime_type": map[string]any{"type": "string", "description": "MIME type (auto-detected if omitted)."},
					"base64":    map[string]any{"type": "string", "description": "Base64-encoded file content."},
				},
				"required": []string{"filename", "base64"},
			}),
		),
		mcp.WithDestructiveHintAnnotation(false),
	), s.handleComposeMessage)

	srv.AddTool(mcp.NewTool("reply",
		mcp.WithDescription("Reply to a message. The original is quoted automatically."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID of the message to reply to.")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Reply body text (placed above the quoted original).")),
		mcp.WithBoolean("reply_all", mcp.Description("Reply to all recipients (default false).")),
		mcp.WithBoolean("markdown", mcp.Description("Render body as Markdown (default false).")),
		mcp.WithDestructiveHintAnnotation(false),
	), s.handleReply)

	srv.AddTool(mcp.NewTool("forward",
		mcp.WithDescription("Forward a message. The original body and attachments are included automatically."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID of the message to forward.")),
		mcp.WithArray("to", mcp.Required(), mcp.Description("Recipient addresses."), mcp.WithStringItems()),
		mcp.WithString("body", mcp.Description("Introductory body text (placed above the forwarded original).")),
		mcp.WithBoolean("markdown", mcp.Description("Render body as Markdown (default false).")),
		mcp.WithArray("extra_attachments", mcp.Description("Additional attachments to include."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"filename":  map[string]any{"type": "string"},
					"mime_type": map[string]any{"type": "string"},
					"base64":    map[string]any{"type": "string", "description": "Base64-encoded content."},
				},
				"required": []string{"filename", "base64"},
			}),
		),
		mcp.WithDestructiveHintAnnotation(false),
	), s.handleForward)

	srv.AddTool(mcp.NewTool("mark_read",
		mcp.WithDescription("Mark a message as read."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID.")),
	), s.handleMarkRead)

	srv.AddTool(mcp.NewTool("mark_unread",
		mcp.WithDescription("Mark a message as unread."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID.")),
	), s.handleMarkUnread)

	srv.AddTool(mcp.NewTool("flag",
		mcp.WithDescription("Flag (star) a message."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID.")),
	), s.handleFlag)

	srv.AddTool(mcp.NewTool("unflag",
		mcp.WithDescription("Remove the flag (star) from a message."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID.")),
	), s.handleUnflag)

	srv.AddTool(mcp.NewTool("delete_message",
		mcp.WithDescription("Move a message to the Trash folder."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name.")),
		mcp.WithString("role", mcp.Description("Semantic folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID.")),
		mcp.WithDestructiveHintAnnotation(true),
	), s.handleDeleteMessage)

	srv.AddTool(mcp.NewTool("move_message",
		mcp.WithDescription("Move a message to a different folder."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Source folder name.")),
		mcp.WithString("role", mcp.Description("Source folder role.")),
		mcp.WithInteger("uid", mcp.Required(), mcp.Description("IMAP UID.")),
		mcp.WithString("dest_folder", mcp.Required(), mcp.Description("Destination folder name (exact IMAP mailbox name).")),
	), s.handleMoveMessage)
}
