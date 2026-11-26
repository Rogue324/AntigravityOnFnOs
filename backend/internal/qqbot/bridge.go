package qqbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/agy"
	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

const (
	maxAttachmentBytes = 20 << 20 // 20 MB
	chatTurnTimeout    = 10 * time.Minute
)

var (
	mentionTagRe  = regexp.MustCompile(`<@!?\d+>`)
	safeFileSegRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

type pendingApproval struct {
	SessionID string
	RequestID string
	Title     string
	Options   []agy.PermissionOption
	CreatedAt time.Time
}

type activeTurn struct {
	Cancel    context.CancelFunc
	SessionID string
	StartedAt time.Time
}

type bridge struct {
	mgr        *Manager
	httpClient *http.Client

	mu        sync.Mutex
	turns     map[string]*activeTurn
	approvals map[string]*pendingApproval
}

func newBridge(mgr *Manager) *bridge {
	return &bridge{
		mgr:        mgr,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		turns:      make(map[string]*activeTurn),
		approvals:  make(map[string]*pendingApproval),
	}
}

func (b *bridge) handleInbound(ctx context.Context, client *Client, msg InboundMessage) {
	cfg := b.mgr.getConfigCopy()
	if !cfg.Enabled {
		return
	}

	// Remember latest C2C sender for connection test if allowed.
	if allowed, reason := b.checkAccess(cfg, msg); !allowed {
		b.mgr.appendLog("[bridge] 拦截未授权消息 (%s sender=%s group=%s): %s", msg.Kind, msg.SenderID, msg.GroupOpenID, reason)
		if msg.Kind == "c2c" && reason != "" {
			_ = client.SendText(ctx, "c2c", msg.SenderID, msg.MessageID, reason, 0)
		}
		return
	}

	if msg.Kind == "c2c" && msg.SenderID != "" {
		b.mgr.recordLastC2CUser(msg.SenderID)
	}

	text := cleanInboundContent(msg.Content)
	// Include voice ASR text if QQ delivered audio transcription
	for _, att := range msg.Attachments {
		if strings.TrimSpace(att.ASRReferText) != "" {
			if text != "" {
				text += "\n"
			}
			text += strings.TrimSpace(att.ASRReferText)
		}
	}

	convKey := msg.ConversationKey()

	// 1. If a tool permission approval is pending for this conversation, check if this message answers it.
	if b.tryHandleApprovalReply(ctx, client, convKey, msg, text) {
		return
	}

	// 2. Handle slash commands (including /stop while a turn is running).
	if strings.HasPrefix(text, "/") {
		b.handleSlashCommand(ctx, client, convKey, msg, text)
		return
	}

	if text == "" && len(msg.Attachments) == 0 {
		return
	}

	// 3. Reject overlapping normal turns for the same conversation.
	b.mu.Lock()
	if _, busy := b.turns[convKey]; busy {
		b.mu.Unlock()
		_ = client.SendText(ctx, msg.Kind, msg.TargetID(), msg.MessageID, "当前会话正在执行任务中，请稍候；如需中止请发送 /stop。", 0)
		return
	}
	turnCtx, cancel := context.WithTimeout(ctx, chatTurnTimeout)
	b.turns[convKey] = &activeTurn{
		Cancel:    cancel,
		StartedAt: time.Now(),
	}
	b.mu.Unlock()

	defer func() {
		cancel()
		b.mu.Lock()
		delete(b.turns, convKey)
		delete(b.approvals, convKey)
		b.mu.Unlock()
	}()

	// Send typing indicator for C2C chats (non-fatal).
	if msg.Kind == "c2c" {
		go func() {
			notifyCtx, c := context.WithTimeout(turnCtx, 5*time.Second)
			defer c()
			_ = client.SendInputNotify(notifyCtx, msg.SenderID, msg.MessageID, 60)
		}()
	}

	sess, err := b.getOrCreateSession(convKey, cfg, text, len(msg.Attachments) > 0)
	if err != nil {
		b.mgr.appendLog("[bridge] 创建/读取会话失败: %v", err)
		_ = client.SendText(ctx, msg.Kind, msg.TargetID(), msg.MessageID, "会话初始化失败："+err.Error(), 0)
		return
	}

	b.mu.Lock()
	if t, ok := b.turns[convKey]; ok {
		t.SessionID = sess.ID
	}
	b.mu.Unlock()

	// Download any QQ attachments to local uploads directory so agy can read them.
	var savedAttachments []session.Attachment
	if len(msg.Attachments) > 0 {
		savedAttachments = b.downloadAttachments(turnCtx, sess.ID, msg.Attachments)
	}

	// Auto-title on first message.
	if len(sess.Messages) == 0 && (sess.Title == "" || sess.Title == "新对话" || sess.Title == "[QQ] 新对话") {
		titleSrc := text
		if titleSrc == "" && len(savedAttachments) > 0 {
			titleSrc = "附件: " + savedAttachments[0].Name
		}
		if titleSrc != "" {
			if updated, err := b.mgr.sessStore.Update(sess.ID, func(cur *session.Session) {
				cur.Title = "[QQ] " + truncateString(titleSrc, 24)
			}); err == nil {
				sess = updated
			}
		}
	}

	// Persist user message to shared Web Chat session store.
	_, _ = b.mgr.sessStore.AppendMessage(sess.ID, session.Message{
		Role:        "user",
		Content:     text,
		Attachments: savedAttachments,
	})

	effectivePrompt := text
	effectiveExtraDirs := append([]string(nil), sess.ExtraDirs...)
	if len(savedAttachments) > 0 {
		var sb strings.Builder
		if effectivePrompt != "" {
			sb.WriteString(effectivePrompt)
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("请查看并分析以下附件：\n\n")
		}
		sb.WriteString("附带文件（位于本地磁盘，请直接读取）：\n")
		for _, att := range savedAttachments {
			kind := "文件"
			if att.IsImage {
				kind = "图片"
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", kind, att.Name, att.Path))
		}
		effectivePrompt = sb.String()

		uploadDir := b.uploadsBaseDir()
		hasDir := false
		for _, d := range effectiveExtraDirs {
			if d == uploadDir {
				hasDir = true
				break
			}
		}
		if !hasDir {
			effectiveExtraDirs = append(effectiveExtraDirs, uploadDir)
		}
	}

	result, runErr := b.mgr.agyMgr.RunChat(turnCtx, agy.ChatRequest{
		SessionID:      sess.ID,
		Prompt:         effectivePrompt,
		ConversationID: sess.ConversationID,
		Workspace:      sess.Workspace,
		ExtraDirs:      effectiveExtraDirs,
		Model:          sess.Model,
		Effort:         sess.Effort,
		PermissionMode: sess.PermissionMode,
	}, func(ev agy.ChatEvent) {
		switch ev.Type {
		case "permission_request":
			if ev.PermissionRequest != nil {
				b.presentPermissionPrompt(ctx, client, convKey, msg, ev.PermissionRequest)
			}
		case "permission_resolved":
			b.mu.Lock()
			delete(b.approvals, convKey)
			b.mu.Unlock()
		}
	})

	// Persist assistant message & conversation_id.
	if result != nil {
		if result.ConversationID != "" && result.ConversationID != sess.ConversationID {
			_, _ = b.mgr.sessStore.Update(sess.ID, func(cur *session.Session) {
				cur.ConversationID = result.ConversationID
			})
		}
		if result.Message.Content != "" || result.Message.Thinking != "" || len(result.Message.ToolCalls) > 0 || result.Message.Error != "" {
			_, _ = b.mgr.sessStore.AppendMessage(sess.ID, result.Message)
		}
	}

	replyCtx, replyCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer replyCancel()

	if runErr != nil {
		if turnCtx.Err() == context.Canceled {
			_ = client.SendText(replyCtx, msg.Kind, msg.TargetID(), msg.MessageID, "已停止当前任务。", 0)
			return
		}
		errMsg := runErr.Error()
		if result != nil && result.Message.Error != "" {
			errMsg = result.Message.Error
		}
		b.mgr.appendLog("[bridge] 执行失败 (%s): %s", convKey, errMsg)
		_ = client.SendText(replyCtx, msg.Kind, msg.TargetID(), msg.MessageID, "执行出错："+errMsg, 0)
		return
	}

	replyText := ""
	if result != nil {
		replyText = strings.TrimSpace(result.Message.Content)
		if replyText == "" && result.Message.Error != "" {
			replyText = "执行出错：" + result.Message.Error
		}
	}
	if replyText == "" {
		replyText = "任务已完成（无文本输出）。"
	}

	if err := client.SendMarkdownReply(replyCtx, msg.Kind, msg.TargetID(), msg.MessageID, replyText); err != nil {
		b.mgr.appendLog("[bridge] 发送 QQ 回复失败 (%s): %v", convKey, err)
	}
}

func (b *bridge) checkAccess(cfg BotConfig, msg InboundMessage) (bool, string) {
	if msg.Kind == "c2c" {
		if !cfg.AllowC2C {
			return false, "管理员已关闭 QQ 私聊访问。"
		}
		if len(cfg.AllowedUsers) > 0 {
			if containsTrimmed(cfg.AllowedUsers, msg.SenderID) {
				return true, ""
			}
			if cfg.OwnerUserOpenID != "" && cfg.OwnerUserOpenID == msg.SenderID {
				return true, ""
			}
			return false, fmt.Sprintf("您不在私聊白名单中。\n您的 OpenID: %s", msg.SenderID)
		}
		if cfg.RestrictToOwner && cfg.OwnerUserOpenID != "" && cfg.OwnerUserOpenID != "*" {
			if msg.SenderID != cfg.OwnerUserOpenID {
				return false, fmt.Sprintf("当前机器人仅限绑定主人私聊使用。\n您的 OpenID: %s", msg.SenderID)
			}
		}
		return true, ""
	}

	if msg.Kind == "group" {
		if !cfg.AllowGroup {
			return false, ""
		}
		if len(cfg.AllowedGroups) > 0 && !containsTrimmed(cfg.AllowedGroups, msg.GroupOpenID) {
			return false, ""
		}
		return true, ""
	}

	return false, ""
}

func (b *bridge) presentPermissionPrompt(ctx context.Context, client *Client, convKey string, msg InboundMessage, pr *agy.PermissionRequest) {
	b.mu.Lock()
	b.approvals[convKey] = &pendingApproval{
		SessionID: pr.SessionID,
		RequestID: pr.RequestID,
		Title:     pr.Title,
		Options:   append([]agy.PermissionOption(nil), pr.Options...),
		CreatedAt: time.Now(),
	}
	b.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("【需要权限审批】\n")
	if pr.Title != "" {
		sb.WriteString("操作：")
		sb.WriteString(pr.Title)
		sb.WriteByte('\n')
	}
	if pr.Input != "" {
		sb.WriteString("参数：")
		sb.WriteString(truncateString(strings.TrimSpace(pr.Input), 300))
		sb.WriteByte('\n')
	}
	sb.WriteString("\n请直接回复序号确认：\n")
	for i, opt := range pr.Options {
		label := opt.Name
		if label == "" {
			label = opt.Kind
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, label))
	}
	sb.WriteString("0. 取消 / 拒绝")

	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.SendText(sendCtx, msg.Kind, msg.TargetID(), msg.MessageID, sb.String(), 0); err != nil {
		b.mgr.appendLog("[bridge] 发送权限审批提示失败: %v", err)
	}
}

func (b *bridge) tryHandleApprovalReply(ctx context.Context, client *Client, convKey string, msg InboundMessage, text string) bool {
	b.mu.Lock()
	pending, ok := b.approvals[convKey]
	b.mu.Unlock()
	if !ok || pending == nil {
		return false
	}

	norm := strings.ToLower(strings.TrimSpace(text))
	if norm == "" {
		return false
	}

	var decision agy.PermissionDecision
	matched := false
	choiceLabel := ""

	switch norm {
	case "0", "n", "no", "取消", "拒绝", "reject", "cancel":
		decision = agy.PermissionDecision{Cancelled: true}
		choiceLabel = "已拒绝/取消"
		matched = true
	case "y", "yes", "同意", "允许", "allow":
		if len(pending.Options) > 0 {
			decision = agy.PermissionDecision{OptionID: pending.Options[0].OptionID}
			choiceLabel = pending.Options[0].Name
			if choiceLabel == "" {
				choiceLabel = "已允许"
			}
			matched = true
		}
	default:
		if idx, err := strconv.Atoi(norm); err == nil && idx >= 1 && idx <= len(pending.Options) {
			opt := pending.Options[idx-1]
			decision = agy.PermissionDecision{OptionID: opt.OptionID}
			choiceLabel = opt.Name
			if choiceLabel == "" {
				choiceLabel = opt.Kind
			}
			matched = true
		}
	}

	if !matched {
		return false
	}

	b.mu.Lock()
	delete(b.approvals, convKey)
	b.mu.Unlock()

	if err := b.mgr.agyMgr.ResolvePermission(pending.SessionID, pending.RequestID, decision); err != nil {
		_ = client.SendText(ctx, msg.Kind, msg.TargetID(), msg.MessageID, "审批处理失败（可能已超时或已在 Web 端处理）："+err.Error(), 0)
		return true
	}

	_ = client.SendText(ctx, msg.Kind, msg.TargetID(), msg.MessageID, "已提交审批选择："+choiceLabel, 0)
	return true
}

func (b *bridge) handleSlashCommand(ctx context.Context, client *Client, convKey string, msg InboundMessage, raw string) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return
	}
	cmd := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))

	reply := func(text string) {
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		_ = client.SendText(sendCtx, msg.Kind, msg.TargetID(), msg.MessageID, text, 0)
	}

	cfg := b.mgr.getConfigCopy()
	globalCfg := b.mgr.cfgStore.Get()

	switch cmd {
	case "/help", "/h":
		reply(strings.Join([]string{
			"Antigravity for fnOS - QQ 机器人命令：",
			"",
			"直接发送文字、图片或文件即可对话（与 Web Chat 共享会话）。",
			"/new [标题]  开启全新会话",
			"/status  查看连接状态与当前会话信息",
			"/workspace [路径]  查看或切换当前会话工作区",
			"/model [模型ID]  列出可用模型或切换当前会话模型",
			"/permission [default|accept-edits|plan|bypass]  查看或切换权限模式",
			"/stop  中止当前正在执行的任务",
			"/help  显示本帮助",
		}, "\n"))

	case "/stop", "/cancel":
		b.mu.Lock()
		turn, busy := b.turns[convKey]
		b.mu.Unlock()
		if !busy {
			reply("当前没有正在执行的任务。")
			return
		}
		turn.Cancel()
		reply("正在停止当前任务...")

	case "/new":
		b.mu.Lock()
		_, busy := b.turns[convKey]
		b.mu.Unlock()
		if busy {
			reply("当前会话正在执行任务，请先发送 /stop 停止后再开启新会话。")
			return
		}
		title := "[QQ] 新对话"
		if arg != "" {
			title = "[QQ] " + truncateString(arg, 24)
		}
		ws := firstNonEmpty(cfg.Workspace, globalCfg.DefaultWorkspace)
		model := firstNonEmpty(cfg.Model, globalCfg.DefaultModel)
		effort := firstNonEmpty(cfg.Effort, globalCfg.DefaultEffort)
		perm := firstNonEmpty(cfg.PermissionMode, globalCfg.DefaultPermissionMode, "default")

		sess, err := b.mgr.sessStore.Create(title, ws, nil, model, effort, perm)
		if err != nil {
			reply("创建新会话失败：" + err.Error())
			return
		}
		b.mgr.setConversationSession(convKey, sess.ID)
		reply(fmt.Sprintf("已开启新会话：%s\n工作区：%s\n权限模式：%s", sess.Title, sess.Workspace, sess.PermissionMode))

	case "/status":
		sessID := b.mgr.getConversationSession(convKey)
		var lines []string
		lines = append(lines, "QQ 机器人运行正常。")
		if msg.Kind == "c2c" {
			lines = append(lines, "您的 OpenID："+msg.SenderID)
		} else {
			lines = append(lines, "群 OpenID："+msg.GroupOpenID)
		}
		if sessID != "" {
			if sess, err := b.mgr.sessStore.Get(sessID); err == nil {
				lines = append(lines,
					"当前会话："+sess.Title+" ("+sess.ID+")",
					"工作区："+sess.Workspace,
					"模型："+firstNonEmpty(sess.Model, "默认"),
					"权限模式："+firstNonEmpty(sess.PermissionMode, "default"),
					fmt.Sprintf("消息数：%d", len(sess.Messages)),
				)
			} else {
				lines = append(lines, "当前尚未绑定会话，直接发消息即可自动创建。")
			}
		} else {
			lines = append(lines, "当前尚未绑定会话，直接发消息即可自动创建。")
		}
		agyStatus := b.mgr.agyMgr.GetStatus(ctx)
		if agyStatus.Installed {
			lines = append(lines, "agy CLI：已安装 "+agyStatus.Version)
		} else {
			lines = append(lines, "agy CLI：未安装（请先在 Web 设置中心安装）")
		}
		reply(strings.Join(lines, "\n"))

	case "/workspace", "/ws":
		sess, err := b.getOrCreateSession(convKey, cfg, "", false)
		if err != nil {
			reply("获取会话失败：" + err.Error())
			return
		}
		if arg == "" {
			reply(fmt.Sprintf("当前工作区：%s\n用法：/workspace /vol1/your-dir", sess.Workspace))
			return
		}
		target := filepath.Clean(arg)
		st, statErr := os.Stat(target)
		if statErr != nil || !st.IsDir() {
			reply("目录不存在或无权限访问：" + target)
			return
		}
		if _, err := b.mgr.sessStore.Update(sess.ID, func(cur *session.Session) {
			cur.Workspace = target
		}); err != nil {
			reply("更新工作区失败：" + err.Error())
			return
		}
		b.mgr.agyMgr.CloseSession(sess.ID)
		reply("已切换当前会话工作区为：" + target)

	case "/model", "/models":
		sess, err := b.getOrCreateSession(convKey, cfg, "", false)
		if err != nil {
			reply("获取会话失败：" + err.Error())
			return
		}
		if arg == "" || cmd == "/models" {
			models, listErr := b.mgr.agyMgr.ListModels(ctx)
			var lines []string
			lines = append(lines, "当前模型："+firstNonEmpty(sess.Model, "默认"))
			if listErr == nil && len(models) > 0 {
				lines = append(lines, "", "可用模型：")
				for i, m := range models {
					lines = append(lines, fmt.Sprintf("%d. %s", i+1, m.ID))
				}
				lines = append(lines, "", "切换用法：/model <序号或模型ID> 或 /model default")
			} else {
				lines = append(lines, "切换用法：/model <模型ID> 或 /model default")
			}
			reply(strings.Join(lines, "\n"))
			return
		}

		newModel := arg
		if strings.EqualFold(newModel, "default") || newModel == "--default" {
			newModel = ""
		} else if idx, err := strconv.Atoi(newModel); err == nil && idx >= 1 {
			if models, err := b.mgr.agyMgr.ListModels(ctx); err == nil && idx <= len(models) {
				newModel = models[idx-1].ID
			}
		}
		if _, err := b.mgr.sessStore.Update(sess.ID, func(cur *session.Session) {
			cur.Model = newModel
		}); err != nil {
			reply("切换模型失败：" + err.Error())
			return
		}
		reply("已切换当前会话模型为：" + firstNonEmpty(newModel, "默认"))

	case "/permission", "/perm":
		sess, err := b.getOrCreateSession(convKey, cfg, "", false)
		if err != nil {
			reply("获取会话失败：" + err.Error())
			return
		}
		if arg == "" {
			reply(fmt.Sprintf("当前权限模式：%s\n可选值：default | accept-edits | plan | bypass\n用法：/permission accept-edits", firstNonEmpty(sess.PermissionMode, "default")))
			return
		}
		mode := strings.ToLower(strings.TrimSpace(arg))
		switch mode {
		case "default", "accept-edits", "plan", "bypass":
			if mode == "bypass" && !globalCfg.AllowDangerousSkip {
				reply("全局设置中未开启「允许跳过权限确认 (AllowDangerousSkip)」，无法切换到 bypass 模式。")
				return
			}
		default:
			reply("无效的权限模式，可选：default | accept-edits | plan | bypass")
			return
		}
		if _, err := b.mgr.sessStore.Update(sess.ID, func(cur *session.Session) {
			cur.PermissionMode = mode
		}); err != nil {
			reply("切换权限模式失败：" + err.Error())
			return
		}
		reply("已切换当前会话权限模式为：" + mode)

	default:
		reply("未知命令：" + cmd + "\n发送 /help 查看可用命令。")
	}
}

func (b *bridge) getOrCreateSession(convKey string, cfg BotConfig, firstText string, hasAtt bool) (*session.Session, error) {
	if existingID := b.mgr.getConversationSession(convKey); existingID != "" {
		if sess, err := b.mgr.sessStore.Get(existingID); err == nil {
			return sess, nil
		}
	}

	globalCfg := b.mgr.cfgStore.Get()
	ws := firstNonEmpty(cfg.Workspace, globalCfg.DefaultWorkspace)
	model := firstNonEmpty(cfg.Model, globalCfg.DefaultModel)
	effort := firstNonEmpty(cfg.Effort, globalCfg.DefaultEffort)
	perm := firstNonEmpty(cfg.PermissionMode, globalCfg.DefaultPermissionMode, "default")

	title := "[QQ] 新对话"
	if firstText != "" {
		title = "[QQ] " + truncateString(firstText, 24)
	} else if hasAtt {
		title = "[QQ] 附件对话"
	}

	sess, err := b.mgr.sessStore.Create(title, ws, nil, model, effort, perm)
	if err != nil {
		return nil, err
	}
	b.mgr.setConversationSession(convKey, sess.ID)
	return sess, nil
}

func (b *bridge) uploadsBaseDir() string {
	return filepath.Join(b.mgr.cfgStore.Paths().DataDir, "uploads")
}

func (b *bridge) downloadAttachments(ctx context.Context, sessionID string, inbound []InboundAttachment) []session.Attachment {
	dir := filepath.Join(b.uploadsBaseDir(), sanitizeFilename(sessionID))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		b.mgr.appendLog("[bridge] 创建附件目录失败: %v", err)
		return nil
	}

	var out []session.Attachment
	for _, att := range inbound {
		rawURL := strings.TrimSpace(att.URL)
		if rawURL == "" {
			continue
		}
		if strings.HasPrefix(rawURL, "//") {
			rawURL = "https:" + rawURL
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			continue
		}
		resp, err := b.httpClient.Do(req)
		if err != nil {
			b.mgr.appendLog("[bridge] 下载 QQ 附件失败: %v", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			b.mgr.appendLog("[bridge] 下载 QQ 附件返回 HTTP %d", resp.StatusCode)
			continue
		}

		head := make([]byte, 512)
		n, _ := io.ReadFull(resp.Body, head)
		head = head[:n]

		mimeType := strings.TrimSpace(strings.Split(att.ContentType, ";")[0])
		if mimeType == "" || mimeType == "application/octet-stream" {
			mimeType = http.DetectContentType(head)
		}

		origName := strings.TrimSpace(att.Filename)
		if origName == "" {
			ext := ".bin"
			if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
				ext = exts[0]
			}
			origName = "qq_attachment" + ext
		}

		isImage := strings.HasPrefix(strings.ToLower(mimeType), "image/")
		switch strings.ToLower(filepath.Ext(origName)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
			isImage = true
		}

		randBuf := make([]byte, 6)
		_, _ = rand.Read(randBuf)
		id := hex.EncodeToString(randBuf)
		storedName := id + "_" + sanitizeFilename(origName)
		destPath := filepath.Join(dir, storedName)

		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
		if err != nil {
			resp.Body.Close()
			continue
		}
		w1, err1 := f.Write(head)
		w2, err2 := io.Copy(f, io.LimitReader(resp.Body, maxAttachmentBytes))
		_ = f.Close()
		resp.Body.Close()
		if err1 != nil || err2 != nil {
			_ = os.Remove(destPath)
			continue
		}

		out = append(out, session.Attachment{
			ID:       id,
			Name:     origName,
			Path:     destPath,
			URL:      "/api/uploads/" + sanitizeFilename(sessionID) + "/" + storedName,
			MimeType: mimeType,
			Size:     int64(w1) + w2,
			IsImage:  isImage,
		})
	}
	return out
}

func cleanInboundContent(raw string) string {
	cleaned := mentionTagRe.ReplaceAllString(raw, " ")
	return strings.TrimSpace(cleaned)
}

func sanitizeFilename(s string) string {
	s = strings.TrimSpace(filepath.Base(s))
	s = safeFileSegRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._")
	if s == "" {
		return "file"
	}
	if len(s) > 80 {
		ext := filepath.Ext(s)
		s = s[:80-len(ext)] + ext
	}
	return s
}

func containsTrimmed(list []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range list {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
