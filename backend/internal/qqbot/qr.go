package qqbot

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultQRHost       = "q.qq.com"
	defaultQRSource     = "antigravity-fnos"
	qrPollInterval      = 2 * time.Second
	qrHTTPTimeout       = 10 * time.Second
	qrTTL               = 5 * time.Minute
	bindStatusNone      = 0
	bindStatusPending   = 1
	bindStatusCompleted = 2
	bindStatusExpired   = 3
)

// QRCredentials holds the decrypted bot credentials returned after scanning the QQ QR code.
type QRCredentials struct {
	AppID      string
	AppSecret  string
	UserOpenID string
}

// QRClient implements Tencent's QQ Bot QR code provisioning protocol (q.qq.com/lite/*).
type QRClient struct {
	host       string
	source     string
	httpClient *http.Client
}

func NewQRClient(httpClient *http.Client) *QRClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: qrHTTPTimeout}
	}
	return &QRClient{
		host:       defaultQRHost,
		source:     defaultQRSource,
		httpClient: httpClient,
	}
}

type bindTask struct {
	TaskID string
	Key    string // base64-encoded 32-byte AES-256 key
}

type pollBindResp struct {
	Status           int
	BotAppID         string
	BotEncryptSecret string
	UserOpenID       string
}

func generateBindKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// DecryptBindSecret decrypts bot_encrypt_secret using AES-256-GCM.
// Wire format: [12-byte nonce][ciphertext][16-byte GCM auth tag], base64-encoded.
func DecryptBindSecret(encryptedBase64, keyBase64 string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return "", fmt.Errorf("invalid bind key: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return "", fmt.Errorf("invalid encrypted secret: %w", err)
	}
	if len(raw) < 12+16 {
		return "", errors.New("encrypted secret payload too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher init failed: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm init failed: %w", err)
	}
	nonce := raw[:12]
	ciphertextWithTag := raw[12:]
	plain, err := gcm.Open(nil, nonce, ciphertextWithTag, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret failed: %w", err)
	}
	return string(plain), nil
}

func (c *QRClient) buildConnectURL(taskID string) string {
	return fmt.Sprintf(
		"https://%s/qqbot/openclaw/connect.html?task_id=%s&source=%s&_wv=2",
		c.host,
		url.QueryEscape(taskID),
		url.QueryEscape(c.source),
	)
}

func (c *QRClient) postJSON(ctx context.Context, endpoint string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, string(raw))
	}
	return json.Unmarshal(raw, out)
}

func (c *QRClient) createBindTask(ctx context.Context) (*bindTask, error) {
	key, err := generateBindKey()
	if err != nil {
		return nil, err
	}
	var resp struct {
		RetCode int    `json:"retcode"`
		Msg     string `json:"msg"`
		Data    struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	endpoint := fmt.Sprintf("https://%s/lite/create_bind_task", c.host)
	if err := c.postJSON(ctx, endpoint, map[string]string{"key": key}, &resp); err != nil {
		return nil, err
	}
	if resp.RetCode != 0 {
		msg := resp.Msg
		if msg == "" {
			msg = fmt.Sprintf("retcode=%d", resp.RetCode)
		}
		return nil, fmt.Errorf("create_bind_task failed: %s", msg)
	}
	if resp.Data.TaskID == "" {
		return nil, errors.New("create_bind_task: missing task_id")
	}
	return &bindTask{TaskID: resp.Data.TaskID, Key: key}, nil
}

func (c *QRClient) pollBindResult(ctx context.Context, taskID string) (*pollBindResp, error) {
	var resp struct {
		RetCode int    `json:"retcode"`
		Msg     string `json:"msg"`
		Data    struct {
			Status           int             `json:"status"`
			BotAppID         json.RawMessage `json:"bot_appid"`
			BotEncryptSecret string          `json:"bot_encrypt_secret"`
			UserOpenID       string          `json:"user_openid"`
		} `json:"data"`
	}
	endpoint := fmt.Sprintf("https://%s/lite/poll_bind_result", c.host)
	if err := c.postJSON(ctx, endpoint, map[string]string{"task_id": taskID}, &resp); err != nil {
		return nil, err
	}
	if resp.RetCode != 0 {
		msg := resp.Msg
		if msg == "" {
			msg = fmt.Sprintf("retcode=%d", resp.RetCode)
		}
		return nil, fmt.Errorf("poll_bind_result failed: %s", msg)
	}

	appID := parseFlexibleString(resp.Data.BotAppID)
	return &pollBindResp{
		Status:           resp.Data.Status,
		BotAppID:         appID,
		BotEncryptSecret: resp.Data.BotEncryptSecret,
		UserOpenID:       resp.Data.UserOpenID,
	}, nil
}

func parseFlexibleString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

// RunQRProvisioning runs the full QR bind loop until scanned, cancelled, or failed.
// It automatically refreshes the QR code when Tencent reports expiry.
func (c *QRClient) RunQRProvisioning(
	ctx context.Context,
	onQR func(verificationURL string, expiresAt time.Time),
	onExpired func(),
) (*QRCredentials, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		task, err := c.createBindTask(ctx)
		if err != nil {
			return nil, fmt.Errorf("获取绑定任务失败: %w", err)
		}

		connectURL := c.buildConnectURL(task.TaskID)
		if onQR != nil {
			onQR(connectURL, time.Now().Add(qrTTL))
		}

		creds, expired, err := c.pollUntilDone(ctx, task)
		if err != nil {
			return nil, err
		}
		if expired {
			if onExpired != nil {
				onExpired()
			}
			continue
		}
		return creds, nil
	}
}

func (c *QRClient) pollUntilDone(ctx context.Context, task *bindTask) (*QRCredentials, bool, error) {
	ticker := time.NewTicker(qrPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-ticker.C:
			res, err := c.pollBindResult(ctx, task.TaskID)
			if err != nil {
				// Transient network errors during polling are retried until cancelled.
				continue
			}
			switch res.Status {
			case bindStatusCompleted:
				secret, err := DecryptBindSecret(res.BotEncryptSecret, task.Key)
				if err != nil {
					return nil, false, fmt.Errorf("解密机器人凭据失败: %w", err)
				}
				if res.BotAppID == "" || secret == "" {
					return nil, false, errors.New("腾讯返回了空的机器人凭据")
				}
				return &QRCredentials{
					AppID:      res.BotAppID,
					AppSecret:  secret,
					UserOpenID: res.UserOpenID,
				}, false, nil
			case bindStatusExpired:
				return nil, true, nil
			}
		}
	}
}
