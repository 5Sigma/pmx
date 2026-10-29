package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/5Sigma/pmx/internal/compose"
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	uicompose "github.com/5Sigma/pmx/internal/ui/compose"
)

// — Read tools —

func (s *Server) handleListAccounts(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	out := make([]AccountJSON, len(s.cfg.Accounts))
	for i, a := range s.cfg.Accounts {
		out[i] = toAccountJSON(a)
	}
	return mcp.NewToolResultJSON(map[string]any{"accounts": out})
}

func (s *Server) handleListFolders(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	_, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	folders, err := s.st.ListFolders(accountID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}

	out := make([]FolderJSON, 0, len(folders))
	for _, f := range folders {
		unread, _ := s.st.CountUnread(f.ID)
		total, _ := s.st.CountMessages(f.ID)
		out = append(out, FolderJSON{
			Name:   f.Name,
			Role:   string(f.Role),
			Unread: unread,
			Total:  total,
		})
	}
	return mcp.NewToolResultJSON(map[string]any{"folders": out})
}

func (s *Server) handleGetMessage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	uid := uint32(req.GetInt("uid", 0))
	bodyFormat := req.GetString("body", "text")

	_, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	msg, err := s.st.ByUID(folder.ID, uid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("message uid %d not found: %v", uid, err)), nil
	}

	if msg.HasAttach {
		atts, err := s.st.ListAttachments(msg.ID)
		if err == nil {
			msg.Attachments = atts
		}
	}

	return mcp.NewToolResultJSON(toMessageJSON(msg, accountName, folder.Name, bodyFormat))
}

func (s *Server) handleGetAttachment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	uid := uint32(req.GetInt("uid", 0))
	attachmentID := int64(req.GetInt("attachment_id", 0))

	_, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	msg, err := s.st.ByUID(folder.ID, uid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("message uid %d not found: %v", uid, err)), nil
	}

	atts, err := s.st.ListAttachments(msg.ID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}

	var target *mail.Attachment
	for i := range atts {
		if atts[i].ID == attachmentID {
			target = &atts[i]
			break
		}
	}
	if target == nil {
		return mcp.NewToolResultError(fmt.Sprintf("attachment %d not found", attachmentID)), nil
	}

	diskPath := s.st.AttachmentPath(*target)
	b64, truncated, err := readAttachmentBytes(diskPath, target.SizeBytes)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}

	result := AttachmentGetJSON{
		Filename:  target.Filename,
		MIMEType:  target.MIMEType,
		SizeBytes: target.SizeBytes,
	}
	if truncated {
		result.Path = diskPath
		result.Truncated = true
	} else {
		result.Base64 = b64
	}
	return mcp.NewToolResultJSON(result)
}

func (s *Server) handleListMessages(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	unreadOnly := req.GetBool("unread_only", false)
	limit := req.GetInt("limit", 25)
	if limit > 200 {
		limit = 200
	}
	offset := req.GetInt("offset", 0)

	_, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// When filtering for unread, over-fetch so we can still return up to limit
	// results after the client-side filter.
	fetchLimit := limit
	if unreadOnly {
		fetchLimit = limit * 4
		if fetchLimit > 800 {
			fetchLimit = 800
		}
	}

	msgs, err := s.st.ListFolder(folder.ID, fetchLimit, offset)
	if err != nil {
		return nil, fmt.Errorf("list folder: %w", err)
	}

	out := make([]SearchResultJSON, 0, len(msgs))
	for _, msg := range msgs {
		if unreadOnly && msg.IsRead() {
			continue
		}
		out = append(out, toSearchResultJSON(msg, accountName, folder.Name))
		if len(out) >= limit {
			break
		}
	}
	return mcp.NewToolResultJSON(map[string]any{"messages": out})
}

func (s *Server) handleSearchMessages(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	accountFilter := req.GetString("account", "")
	folderFilter := req.GetString("folder", "")
	limit := req.GetInt("limit", 25)
	if limit > 100 {
		limit = 100
	}

	msgs, err := s.st.Search(query, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Cache folder lookups to avoid repeated queries.
	type folderInfo struct {
		name      string
		accountID int64
	}
	folderCache := map[int64]folderInfo{}
	getFolderInfo := func(folderID int64) (folderInfo, bool) {
		if fi, ok := folderCache[folderID]; ok {
			return fi, true
		}
		f, err := s.st.GetFolder(folderID)
		if err != nil || f == nil {
			return folderInfo{}, false
		}
		fi := folderInfo{name: f.Name, accountID: f.AccountID}
		folderCache[folderID] = fi
		return fi, true
	}

	// Build accountID → name lookup once.
	accountIDToName := map[int64]string{}
	for _, a := range s.cfg.Accounts {
		id, err := s.st.EnsureAccount(a.Name, a.Email)
		if err == nil {
			accountIDToName[id] = a.Name
		}
	}

	var filterAccountID int64
	var filterFolderID int64
	if accountFilter != "" {
		_, id, err := resolveAccount(s.cfg, s.st, accountFilter)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		filterAccountID = id
	}
	if folderFilter != "" && filterAccountID != 0 {
		if f, err := s.st.FolderByName(filterAccountID, folderFilter); err == nil && f != nil {
			filterFolderID = f.ID
		}
	}

	out := make([]SearchResultJSON, 0, len(msgs))
	for _, msg := range msgs {
		fi, ok := getFolderInfo(msg.FolderID)
		if !ok {
			continue
		}
		if filterAccountID != 0 && fi.accountID != filterAccountID {
			continue
		}
		if filterFolderID != 0 && msg.FolderID != filterFolderID {
			continue
		}
		acctName := accountIDToName[fi.accountID]
		out = append(out, toSearchResultJSON(msg, acctName, fi.name))
	}
	return mcp.NewToolResultJSON(map[string]any{"messages": out})
}

func (s *Server) handleUnreadCount(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")

	if folderName != "" || roleName != "" {
		if accountName == "" {
			return mcp.NewToolResultError("account is required when folder or role is specified"), nil
		}
		_, accountID, err := resolveAccount(s.cfg, s.st, accountName)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		folder, err := resolveFolder(s.st, accountID, folderName, roleName)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		n, err := s.st.CountUnread(folder.ID)
		if err != nil {
			return nil, fmt.Errorf("count unread: %w", err)
		}
		return mcp.NewToolResultJSON(map[string]int{"unread": n})
	}

	if accountName != "" {
		_, accountID, err := resolveAccount(s.cfg, s.st, accountName)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		folders, err := s.st.ListFolders(accountID)
		if err != nil {
			return nil, fmt.Errorf("list folders: %w", err)
		}
		total := 0
		for _, f := range folders {
			if f.Role == mail.RoleTrash || f.Role == mail.RoleJunk {
				continue
			}
			n, _ := s.st.CountUnread(f.ID)
			total += n
		}
		return mcp.NewToolResultJSON(map[string]int{"unread": total})
	}

	// No filters — return per-account map.
	result := map[string]int{}
	for _, acct := range s.cfg.Accounts {
		id, err := s.st.EnsureAccount(acct.Name, acct.Email)
		if err != nil {
			continue
		}
		folders, err := s.st.ListFolders(id)
		if err != nil {
			continue
		}
		total := 0
		for _, f := range folders {
			if f.Role == mail.RoleTrash || f.Role == mail.RoleJunk {
				continue
			}
			n, _ := s.st.CountUnread(f.ID)
			total += n
		}
		result[acct.Name] = total
	}
	return mcp.NewToolResultJSON(result)
}

func (s *Server) handleMessageCount(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")

	_, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	n, err := s.st.CountMessages(folder.ID)
	if err != nil {
		return nil, fmt.Errorf("count messages: %w", err)
	}
	return mcp.NewToolResultJSON(map[string]int{"count": n})
}

// — Composition tools —

func (s *Server) handleComposeMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	subject := req.GetString("subject", "")
	body := req.GetString("body", "")
	useMarkdown := req.GetBool("markdown", false)

	acct, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	toAddrs, err := parseAddressSlice(req.GetStringSlice("to", nil))
	if err != nil {
		return mcp.NewToolResultError("to: " + err.Error()), nil
	}
	ccAddrs, _ := parseAddressSlice(req.GetStringSlice("cc", nil))
	bccAddrs, _ := parseAddressSlice(req.GetStringSlice("bcc", nil))

	atts, tempFiles, err := s.decodeAttachments(req.GetArguments()["attachments"])
	defer cleanupTempFiles(tempFiles)
	if err != nil {
		return mcp.NewToolResultError("attachments: " + err.Error()), nil
	}

	return s.buildAndSend(ctx, acct, accountID, uicompose.BuildOpts{
		FromAddr:    acct.Email,
		FromName:    acct.FullName,
		To:          toAddrs,
		Cc:          ccAddrs,
		Bcc:         bccAddrs,
		Subject:     subject,
		Body:        body,
		UseMarkdown: useMarkdown,
		Attachments: atts,
	})
}

func (s *Server) handleReply(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	uid := uint32(req.GetInt("uid", 0))
	body := req.GetString("body", "")
	replyAll := req.GetBool("reply_all", false)
	useMarkdown := req.GetBool("markdown", false)

	acct, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	src, err := s.st.ByUID(folder.ID, uid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("message uid %d not found: %v", uid, err)), nil
	}

	var seed *uicompose.Seed
	if replyAll {
		seed = compose.BuildReplyAll(src, acct, src.BodyText)
	} else {
		seed = compose.BuildReply(src, acct, src.BodyText)
	}

	return s.buildAndSend(ctx, acct, accountID, uicompose.BuildOpts{
		FromAddr:    acct.Email,
		FromName:    acct.FullName,
		To:          seed.To,
		Cc:          seed.Cc,
		Subject:     seed.Subject,
		Body:        body + seed.Body,
		UseMarkdown: useMarkdown,
		InReplyTo:   seed.InReplyTo,
		References:  seed.References,
	})
}

func (s *Server) handleForward(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	uid := uint32(req.GetInt("uid", 0))
	body := req.GetString("body", "")
	useMarkdown := req.GetBool("markdown", false)

	acct, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	src, err := s.st.ByUID(folder.ID, uid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("message uid %d not found: %v", uid, err)), nil
	}

	if src.HasAttach {
		atts, _ := s.st.ListAttachments(src.ID)
		src.Attachments = atts
	}

	seed := compose.BuildForward(src, acct, src.BodyText, s.st.AttachmentPath)

	toAddrs, err := parseAddressSlice(req.GetStringSlice("to", nil))
	if err != nil {
		return mcp.NewToolResultError("to: " + err.Error()), nil
	}

	extraAtts, tempFiles, err := s.decodeAttachments(req.GetArguments()["extra_attachments"])
	defer cleanupTempFiles(tempFiles)
	if err != nil {
		return mcp.NewToolResultError("extra_attachments: " + err.Error()), nil
	}

	return s.buildAndSend(ctx, acct, accountID, uicompose.BuildOpts{
		FromAddr:    acct.Email,
		FromName:    acct.FullName,
		To:          toAddrs,
		Subject:     seed.Subject,
		Body:        body + seed.Body,
		UseMarkdown: useMarkdown,
		Attachments: append(seed.Attachments, extraAtts...),
		InReplyTo:   seed.InReplyTo,
		References:  seed.References,
	})
}

// — Mutation tools —

func (s *Server) handleMarkRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.doFlagOp(req, func(accountName string, acct config.Account, folderID int64, uid uint32, flags mail.Flag) error {
		if err := s.st.UpdateFlags(folderID, uid, flags|mail.FlagSeen); err != nil {
			return err
		}
		if s.ops != nil {
			_ = s.ops.MarkSeen(accountName, folderID, uid, true)
		}
		return nil
	})
}

func (s *Server) handleMarkUnread(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.doFlagOp(req, func(accountName string, acct config.Account, folderID int64, uid uint32, flags mail.Flag) error {
		if err := s.st.UpdateFlags(folderID, uid, flags&^mail.FlagSeen); err != nil {
			return err
		}
		if s.ops != nil {
			_ = s.ops.MarkSeen(accountName, folderID, uid, false)
		}
		return nil
	})
}

func (s *Server) handleFlag(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.doFlagOp(req, func(accountName string, acct config.Account, folderID int64, uid uint32, flags mail.Flag) error {
		if err := s.st.UpdateFlags(folderID, uid, flags|mail.FlagFlagged); err != nil {
			return err
		}
		if s.ops != nil {
			_ = s.ops.MarkFlagged(accountName, folderID, uid, true)
		}
		return nil
	})
}

func (s *Server) handleUnflag(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.doFlagOp(req, func(accountName string, acct config.Account, folderID int64, uid uint32, flags mail.Flag) error {
		if err := s.st.UpdateFlags(folderID, uid, flags&^mail.FlagFlagged); err != nil {
			return err
		}
		if s.ops != nil {
			_ = s.ops.MarkFlagged(accountName, folderID, uid, false)
		}
		return nil
	})
}

func (s *Server) handleDeleteMessage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	uid := uint32(req.GetInt("uid", 0))

	acct, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.st.DeleteMessageByUID(folder.ID, uid); err != nil {
		return nil, fmt.Errorf("delete message: %w", err)
	}
	if s.ops != nil {
		_ = s.ops.MoveToTrash(acct, folder.ID, uid)
	}
	return mcp.NewToolResultJSON(map[string]bool{"ok": true})
}

func (s *Server) handleMoveMessage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	uid := uint32(req.GetInt("uid", 0))
	destFolder := req.GetString("dest_folder", "")

	if destFolder == "" {
		return mcp.NewToolResultError("dest_folder is required"), nil
	}

	acct, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.st.DeleteMessageByUID(folder.ID, uid); err != nil {
		return nil, fmt.Errorf("delete message (local): %w", err)
	}
	if s.ops != nil {
		_ = s.ops.MoveToFolder(acct, folder.ID, uid, destFolder)
	}
	return mcp.NewToolResultJSON(map[string]bool{"ok": true})
}

// — Shared helpers —

// buildAndSend constructs an EML, enqueues it to the outbox, triggers the
// sender, then polls the outbox row until it is deleted (sent), terminally
// failed, or the deadline is reached (returns "queued").
func (s *Server) buildAndSend(ctx context.Context, acct config.Account, accountID int64, opts uicompose.BuildOpts) (*mcp.CallToolResult, error) {
	eml, err := uicompose.BuildEML(opts)
	if err != nil {
		return nil, fmt.Errorf("build EML: %w", err)
	}

	item := &mail.OutboxItem{AccountID: accountID, RawEML: eml}
	if err := s.st.EnqueueOutbox(item); err != nil {
		return nil, fmt.Errorf("enqueue outbox: %w", err)
	}

	if s.send != nil {
		if err := s.send.Trigger(acct.Name); err != nil {
			s.logger.Warn("Trigger failed", "account", acct.Name, "err", err)
			return mcp.NewToolResultJSON(SendResultJSON{Status: "queued"})
		}
	}

	status, lastErr := s.pollOutbox(ctx, item.ID)
	switch status {
	case "sent":
		return mcp.NewToolResultJSON(SendResultJSON{Status: "sent"})
	case "failed":
		return mcp.NewToolResultError(fmt.Sprintf("send failed: %s", lastErr)), nil
	default:
		return mcp.NewToolResultJSON(SendResultJSON{Status: "queued"})
	}
}

// pollOutbox polls the outbox row for id until it is deleted (sent), hits the
// terminal failure threshold, the context is cancelled, or the timeout lapses.
func (s *Server) pollOutbox(ctx context.Context, id int64) (status, lastErr string) {
	const timeout = 60 * time.Second
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		row, exists, err := s.st.GetOutboxItem(id)
		if err != nil {
			s.logger.Warn("pollOutbox: GetOutboxItem failed", "id", id, "err", err)
			return "queued", ""
		}
		if !exists {
			return "sent", ""
		}
		if row.Attempts >= s.cfg.General.SendMaxAttempts && row.LastError != "" {
			return "failed", row.LastError
		}
		if time.Now().After(deadline) {
			return "queued", ""
		}
		select {
		case <-ctx.Done():
			return "queued", ""
		case <-tick.C:
		}
	}
}

// doFlagOp is the shared implementation for mark_read, mark_unread, flag, unflag.
func (s *Server) doFlagOp(req mcp.CallToolRequest, op func(accountName string, acct config.Account, folderID int64, uid uint32, flags mail.Flag) error) (*mcp.CallToolResult, error) {
	accountName := req.GetString("account", "")
	folderName := req.GetString("folder", "")
	roleName := req.GetString("role", "")
	uid := uint32(req.GetInt("uid", 0))

	acct, accountID, err := resolveAccount(s.cfg, s.st, accountName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	folder, err := resolveFolder(s.st, accountID, folderName, roleName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	msg, err := s.st.ByUID(folder.ID, uid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("message uid %d not found", uid)), nil
	}

	if err := op(accountName, acct, folder.ID, uid, msg.Flags); err != nil {
		return nil, fmt.Errorf("flag op: %w", err)
	}
	return mcp.NewToolResultJSON(map[string]bool{"ok": true})
}

// decodeAttachments decodes base64 attachment inputs into temp files.
// The caller must defer cleanupTempFiles(tempFiles).
func (s *Server) decodeAttachments(raw any) ([]uicompose.Attachment, []string, error) {
	if raw == nil {
		return nil, nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, nil, nil
	}

	var atts []uicompose.Attachment
	var tempFiles []string

	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		filename, _ := m["filename"].(string)
		b64data, _ := m["base64"].(string)
		mimeType, _ := m["mime_type"].(string)

		if filename == "" || b64data == "" {
			return nil, tempFiles, fmt.Errorf("each attachment requires filename and base64")
		}

		data, err := base64.StdEncoding.DecodeString(b64data)
		if err != nil {
			return nil, tempFiles, fmt.Errorf("base64 decode %q: %w", filename, err)
		}

		tmpPath, err := writeTempAttachment(s.attachsDir, filename, data)
		if err != nil {
			return nil, tempFiles, fmt.Errorf("write temp %q: %w", filename, err)
		}
		tempFiles = append(tempFiles, tmpPath)

		att := uicompose.Attachment{
			Path:     tmpPath,
			Filename: filename,
			Size:     int64(len(data)),
		}
		if mimeType != "" {
			att.MIMEType = mimeType
		} else {
			if sniffed, err := uicompose.NewAttachment(tmpPath); err == nil {
				att.MIMEType = sniffed.MIMEType
			}
		}
		atts = append(atts, att)
	}
	return atts, tempFiles, nil
}

func cleanupTempFiles(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// parseAddressSlice parses strings of the form "Name <email>" or plain "email".
func parseAddressSlice(strs []string) ([]mail.Address, error) {
	if len(strs) == 0 {
		return nil, nil
	}
	addrs := make([]mail.Address, 0, len(strs))
	for _, raw := range strs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var name, email string
		if i := strings.Index(raw, "<"); i >= 0 {
			name = strings.TrimSpace(raw[:i])
			email = strings.Trim(raw[i+1:], "> ")
		} else {
			email = raw
		}
		if email == "" {
			return nil, fmt.Errorf("invalid address %q", raw)
		}
		addrs = append(addrs, mail.Address{Name: name, Email: email})
	}
	return addrs, nil
}
