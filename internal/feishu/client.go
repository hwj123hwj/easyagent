package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// Client wraps the Feishu SDK REST client for messaging operations.
type Client struct {
	sdk        *lark.Client
	cardClient *http.Client
	appID      string
	appSecret  string

	// Token cache
	cachedToken    string
	tokenExpiresAt time.Time
	tokenMu        sync.RWMutex
}

// NewClient creates a new Feishu REST client.
func NewClient(appID, appSecret string) *Client {
	sdk := lark.NewClient(appID, appSecret)
	return &Client{sdk: sdk, appID: appID, appSecret: appSecret}
}

const maxTextLen = 25000

// truncateText truncates text to maxTextLen and appends a truncation notice.
func truncateText(text string) string {
	if len(text) <= maxTextLen {
		return text
	}
	return text[:maxTextLen] + "\n\n（已截断）"
}

// ReplyMessage replies to a specific message. Returns the bot message ID.
// If reply fails with code 10003 (original message too old), falls back to SendMessage.
func (c *Client) ReplyMessage(ctx context.Context, messageID, chatID, text string) (string, error) {
	text = truncateText(text)
	content := fmt.Sprintf(`{"text":"%s"}`, escapeJSON(text))

	resp, err := c.sdk.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("text").
			Content(content).
			Build()).
		Build())

	if err != nil {
		if isCode10003(err) {
			slog.Info("reply failed with 10003, falling back to SendMessage", "messageID", messageID)
			return c.SendMessage(ctx, chatID, text, "")
		}
		return "", fmt.Errorf("reply message: %w", err)
	}

	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

// receiveIDType returns the correct ReceiveIdType based on the ID prefix.
// "ou_" → OpenID, "oc_" → ChatID, default → ChatID.
func receiveIDType(id string) string {
	if strings.HasPrefix(id, "ou_") {
		return larkim.CreateMessageV1ReceiveIDTypeOpenId
	}
	return larkim.CreateMessageV1ReceiveIDTypeChatId
}

// SendMessage sends a text message to a chat.
func (c *Client) SendMessage(ctx context.Context, chatID, text, _ string) (string, error) {
	text = truncateText(text)
	content := fmt.Sprintf(`{"text":"%s"}`, escapeJSON(text))

	resp, err := c.sdk.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType(chatID)).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("text").
			Content(content).
			Build()).
		Build())

	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

// UpdateMessage updates the content of a bot-sent message via PATCH.
func (c *Client) UpdateMessage(ctx context.Context, messageID, text string) error {
	text = truncateText(text)
	content := fmt.Sprintf(`{"text":"%s"}`, escapeJSON(text))

	_, err := c.sdk.Im.Message.Patch(ctx, larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(content).
			Build()).
		Build())

	if err != nil {
		slog.Warn("update message failed", "messageID", messageID, "error", err)
	}
	return err
}

// SendMarkdown sends a post-type message with rich text content.
func (c *Client) SendMarkdown(ctx context.Context, chatID, markdown, _ string) (string, error) {
	postContent := mdToPostContent(markdown)

	resp, err := c.sdk.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType(chatID)).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("post").
			Content(postContent).
			Build()).
		Build())

	if err != nil {
		slog.Warn("send markdown failed, falling back to text", "error", err)
		return c.SendMessage(ctx, chatID, markdown, "")
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

// AddReaction adds an emoji reaction to a message. Returns the reaction ID.
func (c *Client) AddReaction(ctx context.Context, messageID, emojiType string) (string, error) {
	resp, err := c.sdk.Im.MessageReaction.Create(ctx, larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(&larkim.Emoji{EmojiType: &emojiType}).
			Build()).
		Build())

	if err != nil {
		slog.Warn("add reaction failed", "messageID", messageID, "error", err)
		return "", err
	}
	if resp.Data == nil || resp.Data.ReactionId == nil {
		return "", nil
	}
	return *resp.Data.ReactionId, nil
}

// RemoveReaction removes an emoji reaction from a message.
func (c *Client) RemoveReaction(ctx context.Context, messageID, reactionID string) error {
	if reactionID == "" {
		return nil
	}

	_, err := c.sdk.Im.MessageReaction.Delete(ctx, larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build())

	if err != nil {
		slog.Warn("remove reaction failed", "messageID", messageID, "reactionID", reactionID, "error", err)
	}
	return err
}

// UploadImage uploads an image file and returns the image key.
func (c *Client) UploadImage(ctx context.Context, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open image: %w", err)
	}
	defer f.Close()

	body, contentType, err := createMultipartBody(f, filepath.Base(filePath), "image_type", "message") //nolint:staticcheck // filepath used for base name
	if err != nil {
		return "", err
	}

	resp, err := c.doUpload(ctx, "https://open.feishu.cn/open-apis/im/v1/images", body, contentType)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			ImageKey string `json:"image_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}
	return result.Data.ImageKey, nil
}

// SendImage sends an image message to a chat.
func (c *Client) SendImage(ctx context.Context, chatID, imageKey string) (string, error) {
	content := fmt.Sprintf(`{"image_key":"%s"}`, imageKey)

	resp, err := c.sdk.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType(chatID)).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("image").
			Content(content).
			Build()).
		Build())

	if err != nil {
		return "", fmt.Errorf("send image: %w", err)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

// UploadFile uploads a file and returns the file key.
func (c *Client) UploadFile(ctx context.Context, filePath, fileName string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	body, contentType, err := createMultipartBody(f, fileName, "file_type", "stream")
	if err != nil {
		return "", err
	}

	resp, err := c.doUpload(ctx, "https://open.feishu.cn/open-apis/im/v1/files", body, contentType)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			FileKey string `json:"file_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}
	return result.Data.FileKey, nil
}

// SendFile sends a file message to a chat.
func (c *Client) SendFile(ctx context.Context, chatID, fileKey string) (string, error) {
	content := fmt.Sprintf(`{"file_key":"%s"}`, fileKey)

	resp, err := c.sdk.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType(chatID)).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("file").
			Content(content).
			Build()).
		Build())

	if err != nil {
		return "", fmt.Errorf("send file: %w", err)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

// CreateGroupChat creates a new Feishu group chat and invites the specified users.
// Returns the new chat_id. Requires im:chat scope.
func (c *Client) CreateGroupChat(ctx context.Context, name, description string, userOpenIDs []string) (string, error) {
	token, err := c.getTenantToken(ctx)
	if err != nil {
		return "", err
	}

	idsJSON, _ := json.Marshal(userOpenIDs)
	body := fmt.Sprintf(`{"name":%s,"description":%s,"user_id_list":%s}`,
		jsonString(name), jsonString(description), string(idsJSON))

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://open.feishu.cn/open-apis/im/v1/chats",
		strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create group chat: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ChatID string `json:"chat_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode create chat response: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("create group chat failed (code %d): %s", result.Code, result.Msg)
	}

	slog.Info("created group chat", "chatID", result.Data.ChatID, "name", name)
	return result.Data.ChatID, nil
}

// doUpload performs an authenticated multipart upload to the Feishu API.
func (c *Client) doUpload(ctx context.Context, url string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)

	// Use SDK's Post method to get a fresh token, then do raw upload
	// Simplification: use the SDK's internal token management via a dummy request
	token, err := c.getTenantToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, string(data[:min(len(data), 200)]))
	}
	return data, nil
}

// getTenantToken obtains a tenant access token with caching (TTL - 60s refresh).
func (c *Client) getTenantToken(ctx context.Context) (string, error) {
	// Check cache first (read lock)
	c.tokenMu.RLock()
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiresAt) {
		token := c.cachedToken
		c.tokenMu.RUnlock()
		return token, nil
	}
	c.tokenMu.RUnlock()

	// Cache miss or expired — fetch new token (write lock)
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double-check after acquiring write lock
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiresAt) {
		return c.cachedToken, nil
	}

	body := fmt.Sprintf(`{"app_id":"%s","app_secret":"%s"}`, c.appID, c.appSecret)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if result.TenantAccessToken == "" {
		return "", fmt.Errorf("get token failed (code %d): %s", result.Code, result.Msg)
	}

	// Cache the token (expire - 60 seconds for safety margin)
	c.cachedToken = result.TenantAccessToken
	c.tokenExpiresAt = time.Now().Add(time.Duration(result.Expire-60) * time.Second)

	return result.TenantAccessToken, nil
}

// isCode10003 checks if the error is a "message too old" (code 10003) error.
func isCode10003(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "10003")
}

// escapeJSON escapes a string for safe embedding in a JSON string value.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// createMultipartBody creates a multipart form body for file uploads.
func createMultipartBody(file *os.File, fileName, fieldName, fieldValue string) (io.Reader, string, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		_ = writer.WriteField(fieldName, fieldValue)
		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, file); err != nil {
			pw.CloseWithError(err)
			return
		}
		writer.Close()
	}()

	return pr, writer.FormDataContentType(), nil
}

// Card requests are bounded even if a remote connection stalls.
func (c *Client) cardHTTPClient() *http.Client {
	if c.cardClient != nil {
		return c.cardClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}
