package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultTokenBaseURL = "https://bots.qq.com"
	defaultAPIBaseURL   = "https://api.sgroup.qq.com"
	qqUserAgent         = "AntigravityOnFnOs-QQBot/1.0"

	markdownRejectionBizCode = 40034090
	maxQQChunkRunes          = 1800
	passiveReplyLimitC2C     = 4
	passiveReplyLimitGroup   = 5
)

// APIError represents a structured HTTP/business error from the QQ Open Platform API.
type APIError struct {
	HTTPStatus int
	BizCode    int
	Path       string
	Message    string
}

func (e *APIError) Error() string {
	if e.BizCode != 0 {
		return fmt.Sprintf("QQ API error [%s] HTTP %d (code %d): %s", e.Path, e.HTTPStatus, e.BizCode, e.Message)
	}
	return fmt.Sprintf("QQ API error [%s] HTTP %d: %s", e.Path, e.HTTPStatus, e.Message)
}

func isMarkdownRejection(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.BizCode == markdownRejectionBizCode
}

// Client handles QQ Open Platform authentication and REST API calls.
type Client struct {
	appID        string
	appSecret    string
	tokenBaseURL string
	apiBaseURL   string
	httpClient   *http.Client

	tokenMu     sync.Mutex
	accessToken string
	tokenExpiry time.Time

	logFn func(string)
}

func NewClient(appID, appSecret string, httpClient *http.Client, logFn func(string)) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		appID:        strings.TrimSpace(appID),
		appSecret:    strings.TrimSpace(appSecret),
		tokenBaseURL: defaultTokenBaseURL,
		apiBaseURL:   defaultAPIBaseURL,
		httpClient:   httpClient,
		logFn:        logFn,
	}
}

func (c *Client) log(format string, args ...any) {
	if c.logFn != nil {
		c.logFn(fmt.Sprintf(format, args...))
	}
}

func (c *Client) ClearTokenCache() {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.accessToken = ""
	c.tokenExpiry = time.Time{}
}

// GetAccessToken returns a cached or freshly fetched QQ Bot access token.
func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.accessToken != "" && time.Now().Add(2*time.Minute).Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	payload, _ := json.Marshal(map[string]string{
		"appId":        c.appID,
		"clientSecret": c.appSecret,
	})
	endpoint := c.tokenBaseURL + "/app/getAppAccessToken"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", qqUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 AccessToken 网络失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var data struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
		Code        int             `json:"code"`
		Message     string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", fmt.Errorf("解析 AccessToken 响应失败 (HTTP %d): %s", resp.StatusCode, truncateString(string(raw), 200))
	}
	if resp.StatusCode != http.StatusOK || data.AccessToken == "" {
		msg := data.Message
		if msg == "" {
			msg = truncateString(string(raw), 200)
		}
		return "", &APIError{
			HTTPStatus: resp.StatusCode,
			BizCode:    data.Code,
			Path:       "/app/getAppAccessToken",
			Message:    msg,
		}
	}

	expiresSec := 7200
	if s := parseFlexibleString(data.ExpiresIn); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			expiresSec = v
		}
	}
	c.accessToken = data.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(expiresSec) * time.Second)
	return c.accessToken, nil
}

func (c *Client) doAPI(ctx context.Context, method, path string, body any, out any) error {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiBaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", qqUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("QQ API 网络错误 [%s]: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody struct {
			Code    int    `json:"code"`
			ErrCode int    `json:"err_code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &errBody)
		bizCode := errBody.Code
		if bizCode == 0 {
			bizCode = errBody.ErrCode
		}
		msg := errBody.Message
		if msg == "" {
			msg = truncateString(string(raw), 200)
		}
		return &APIError{
			HTTPStatus: resp.StatusCode,
			BizCode:    bizCode,
			Path:       path,
			Message:    msg,
		}
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("QQ API 响应解析失败 [%s]: %w", path, err)
		}
	}
	return nil
}

// GetGatewayURL retrieves the WebSocket gateway URL from the QQ Open Platform.
func (c *Client) GetGatewayURL(ctx context.Context) (string, error) {
	var resp struct {
		URL string `json:"url"`
	}
	if err := c.doAPI(ctx, http.MethodGet, "/gateway", nil, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.URL) == "" {
		return "", errors.New("QQ /gateway 返回了空的 WebSocket 地址")
	}
	return resp.URL, nil
}

func messagePath(scope, targetID string) string {
	if scope == "group" {
		return fmt.Sprintf("/v2/groups/%s/messages", targetID)
	}
	return fmt.Sprintf("/v2/users/%s/messages", targetID)
}

func nextMsgSeqSeed() int {
	timePart := int(time.Now().UnixMilli() % 100_000_000)
	r := rand.Intn(65536)
	return (timePart ^ r) & 0xFFFF
}

// SendText sends a plain text message (msg_type=0) as passive reply (if msgID != "") or proactive message.
func (c *Client) SendText(ctx context.Context, scope, targetID, msgID, text string, msgSeq int) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if msgSeq <= 0 {
		msgSeq = nextMsgSeqSeed()
		if msgSeq == 0 {
			msgSeq = 1
		}
	}
	body := map[string]any{
		"content":  text,
		"msg_type": 0,
		"msg_seq":  msgSeq,
	}
	if msgID != "" {
		body["msg_id"] = msgID
	}
	return c.doAPI(ctx, http.MethodPost, messagePath(scope, targetID), body, nil)
}

func isClientRejection(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && apiErr.HTTPStatus != http.StatusTooManyRequests
}

// SendMarkdownWithKeyboard sends a markdown message (msg_type=2) with inline keyboard buttons.
func (c *Client) SendMarkdownWithKeyboard(ctx context.Context, scope, targetID, msgID, markdownContent string, keyboard any, msgSeq int) error {
	markdownContent = strings.TrimSpace(markdownContent)
	if markdownContent == "" {
		return nil
	}
	if msgSeq <= 0 {
		msgSeq = nextMsgSeqSeed()
		if msgSeq == 0 {
			msgSeq = 1
		}
	}
	body := map[string]any{
		"msg_type": 2,
		"markdown": map[string]string{
			"content": markdownContent,
		},
		"keyboard": keyboard,
		"msg_seq":  msgSeq,
	}
	if msgID != "" {
		body["msg_id"] = msgID
	}
	return c.doAPI(ctx, http.MethodPost, messagePath(scope, targetID), body, nil)
}

// SendMarkdown sends a markdown message (msg_type=2).
func (c *Client) SendMarkdown(ctx context.Context, scope, targetID, msgID, markdownContent string, msgSeq int) error {
	markdownContent = strings.TrimSpace(markdownContent)
	if markdownContent == "" {
		return nil
	}
	if msgSeq <= 0 {
		msgSeq = nextMsgSeqSeed()
		if msgSeq == 0 {
			msgSeq = 1
		}
	}
	body := map[string]any{
		"msg_type": 2,
		"markdown": map[string]string{
			"content": markdownContent,
		},
		"msg_seq": msgSeq,
	}
	if msgID != "" {
		body["msg_id"] = msgID
	}
	return c.doAPI(ctx, http.MethodPost, messagePath(scope, targetID), body, nil)
}

// SendInputNotify sends a C2C typing indicator (msg_type=6). Errors are non-fatal.
func (c *Client) SendInputNotify(ctx context.Context, openID, msgID string, seconds int) error {
	if openID == "" {
		return nil
	}
	if seconds <= 0 {
		seconds = 60
	}
	body := map[string]any{
		"msg_type": 6,
		"input_notify": map[string]any{
			"input_type":   1,
			"input_second": seconds,
		},
		"msg_seq": nextMsgSeqSeed() | 1,
	}
	if msgID != "" {
		body["msg_id"] = msgID
	}
	return c.doAPI(ctx, http.MethodPost, messagePath("c2c", openID), body, nil)
}

// SendMarkdownReply chunks long text and sends via msg_type=2 (Markdown),
// automatically falling back to msg_type=0 (plain text) if the bot lacks Markdown permission (40034090).
func (c *Client) SendMarkdownReply(ctx context.Context, scope, targetID, msgID, text string) error {
	chunks := chunkMarkdownText(text, maxQQChunkRunes)
	if len(chunks) == 0 {
		return nil
	}

	firstSeq := nextMsgSeqSeed()
	passiveLimit := passiveReplyLimitC2C
	if scope == "group" {
		passiveLimit = passiveReplyLimitGroup
	}

	overflow := msgID != "" && len(chunks) > passiveLimit
	passiveCount := len(chunks)
	if overflow {
		passiveCount = passiveLimit - 1
	}

	markdownDisabled := false
	sentAny := false

	for idx, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		seq := (firstSeq + idx) & 0xFFFF
		if seq == 0 {
			seq = 1
		}
		deliveryMsgID := msgID
		if overflow && idx >= passiveCount {
			deliveryMsgID = ""
		}

		if !markdownDisabled {
			err := c.SendMarkdown(ctx, scope, targetID, deliveryMsgID, chunk, seq)
			if err == nil {
				sentAny = true
				continue
			}
			if isMarkdownRejection(err) {
				markdownDisabled = true
				c.log("[qqbot] 平台未开通 Markdown 权限 (40034090)，自动回退纯文本发送")
			} else {
				if !sentAny {
					return err
				}
				_ = c.SendText(ctx, scope, targetID, msgID, "回答较长，后续内容未能通过 QQ 完整发送，请回复“继续”或在 Web Chat 中查看。", (firstSeq+len(chunks))&0xFFFF)
				return nil
			}
		}

		plain := stripBasicMarkdown(chunk)
		if plain == "" {
			continue
		}
		if err := c.SendText(ctx, scope, targetID, deliveryMsgID, plain, seq); err != nil {
			if !sentAny {
				return err
			}
			_ = c.SendText(ctx, scope, targetID, msgID, "回答较长，后续内容未能通过 QQ 完整发送，请回复“继续”或在 Web Chat 中查看。", (firstSeq+len(chunks))&0xFFFF)
			return nil
		}
		sentAny = true
	}
	return nil
}

// chunkMarkdownText splits long markdown text into chunks <= maxRunes, preferring paragraph/line boundaries
// and keeping fenced code blocks balanced across chunks.
func chunkMarkdownText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = maxQQChunkRunes
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	var chunks []string
	var cur strings.Builder
	curRunes := 0
	inFence := false
	fenceHeader := ""

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		chunk := cur.String()
		if inFence {
			chunk += "\n```"
		}
		chunk = strings.TrimSpace(chunk)
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		cur.Reset()
		curRunes = 0
		if inFence {
			cur.WriteString(fenceHeader)
			cur.WriteByte('\n')
			curRunes = utf8.RuneCountInString(fenceHeader) + 1
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isFenceLine := strings.HasPrefix(trimmed, "```")

		lineRunes := utf8.RuneCountInString(line) + 1
		if curRunes > 0 && curRunes+lineRunes > maxRunes-12 {
			flush()
		}

		// Handle single very long line by hard-splitting on rune boundaries
		if lineRunes > maxRunes-12 {
			runes := []rune(line)
			step := maxRunes - 24
			if step < 100 {
				step = 100
			}
			for start := 0; start < len(runes); start += step {
				end := start + step
				if end > len(runes) {
					end = len(runes)
				}
				part := string(runes[start:end])
				if curRunes > 0 && curRunes+len(runes[start:end])+1 > maxRunes-12 {
					flush()
				}
				cur.WriteString(part)
				cur.WriteByte('\n')
				curRunes += (end - start) + 1
			}
			continue
		}

		cur.WriteString(line)
		cur.WriteByte('\n')
		curRunes += lineRunes

		if isFenceLine {
			if !inFence {
				inFence = true
				fenceHeader = trimmed
			} else {
				inFence = false
				fenceHeader = ""
			}
		}
	}

	if cur.Len() > 0 {
		chunk := strings.TrimSpace(cur.String())
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

func stripBasicMarkdown(s string) string {
	// Keep code and text readable when QQ rejects msg_type=2 markdown.
	return strings.TrimSpace(s)
}

func truncateString(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
