package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
)

func (adapter *OfficialHTTPAdapter) UploadContentImage(ctx context.Context, token Token, media MediaFile) (ContentImage, error) {
	var response struct {
		URL       string `json:"url"`
		ErrorCode int    `json:"errcode"`
	}
	if err := adapter.postMedia(ctx, "/cgi-bin/media/uploadimg", "media", token, media, &response); err != nil {
		return ContentImage{}, err
	}
	if response.ErrorCode != 0 {
		return ContentImage{}, classifyWeChatError(response.ErrorCode)
	}
	if strings.TrimSpace(response.URL) == "" {
		return ContentImage{}, errors.New("WECHAT_CONTENT_IMAGE_RESPONSE: url is required")
	}
	return ContentImage{URL: response.URL}, nil
}

func (adapter *OfficialHTTPAdapter) UploadCover(ctx context.Context, token Token, media MediaFile) (CoverMedia, error) {
	var response struct {
		MediaID   string `json:"media_id"`
		ErrorCode int    `json:"errcode"`
	}
	if err := adapter.postMedia(ctx, "/cgi-bin/material/add_material?type=image", "media", token, media, &response); err != nil {
		return CoverMedia{}, err
	}
	if response.ErrorCode != 0 {
		return CoverMedia{}, classifyWeChatError(response.ErrorCode)
	}
	if strings.TrimSpace(response.MediaID) == "" {
		return CoverMedia{}, errors.New("WECHAT_COVER_RESPONSE: media_id is required")
	}
	return CoverMedia{MediaID: response.MediaID}, nil
}

func (adapter *OfficialHTTPAdapter) AddDraft(ctx context.Context, token Token, draft WeChatDraft) (RemoteDraft, error) {
	var response struct {
		MediaID   string `json:"media_id"`
		ErrorCode *int   `json:"errcode"`
	}
	if err := adapter.postJSON(ctx, "/cgi-bin/draft/add", token, struct {
		Articles []WeChatDraft `json:"articles"`
	}{Articles: []WeChatDraft{draft}}, &response); err != nil {
		if ambiguousDraftWrite(err) {
			return RemoteDraft{}, NewOutcomeUnknownError(err)
		}
		return RemoteDraft{}, err
	}
	if response.ErrorCode != nil && *response.ErrorCode != 0 {
		return RemoteDraft{}, classifyWeChatError(*response.ErrorCode)
	}
	if strings.TrimSpace(response.MediaID) == "" {
		return RemoteDraft{}, NewOutcomeUnknownError(errors.New("WECHAT_DRAFT_RESPONSE: successful response omitted media_id"))
	}
	return RemoteDraft{MediaID: response.MediaID}, nil
}

func (adapter *OfficialHTTPAdapter) UpdateDraft(ctx context.Context, token Token, update WeChatDraftUpdate) error {
	var response struct {
		ErrorCode *int `json:"errcode"`
	}
	payload := struct {
		MediaID string      `json:"media_id"`
		Index   int         `json:"index"`
		Article WeChatDraft `json:"articles"`
	}{MediaID: update.MediaID, Article: update.Draft}
	if err := adapter.postJSON(ctx, "/cgi-bin/draft/update", token, payload, &response); err != nil {
		if ambiguousDraftWrite(err) {
			return NewOutcomeUnknownError(err)
		}
		return err
	}
	if response.ErrorCode == nil {
		return NewOutcomeUnknownError(errors.New("WECHAT_DRAFT_RESPONSE: update response omitted errcode"))
	}
	if *response.ErrorCode != 0 {
		return classifyWeChatError(*response.ErrorCode)
	}
	return nil
}

func ambiguousDraftWrite(err error) bool {
	message := err.Error()
	return strings.HasPrefix(message, "WECHAT_NETWORK:") ||
		strings.HasPrefix(message, "WECHAT_RESPONSE:") ||
		strings.HasPrefix(message, "WECHAT_HTTP:")
}

func (adapter *OfficialHTTPAdapter) postMedia(ctx context.Context, endpointPath, field string, token Token, media MediaFile, target any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filepath.Base(media.Path)))
	partHeader.Set("Content-Type", media.MediaType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return fmt.Errorf("WECHAT_MEDIA_REQUEST: %w", err)
	}
	if _, err := part.Write(media.Content); err != nil {
		return fmt.Errorf("WECHAT_MEDIA_REQUEST: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("WECHAT_MEDIA_REQUEST: %w", err)
	}
	endpoint, err := adapter.endpoint(endpointPath, token)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return fmt.Errorf("WECHAT_MEDIA_REQUEST: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return adapter.doJSON(request, target)
}

func (adapter *OfficialHTTPAdapter) postJSON(ctx context.Context, endpointPath string, token Token, payload, target any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("WECHAT_DRAFT_REQUEST: %w", err)
	}
	endpoint, err := adapter.endpoint(endpointPath, token)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("WECHAT_DRAFT_REQUEST: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return adapter.doJSON(request, target)
}

func (adapter *OfficialHTTPAdapter) endpoint(endpointPath string, token Token) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(adapter.baseURL, "/") + endpointPath)
	if err != nil {
		return "", fmt.Errorf("WECHAT_ENDPOINT: %w", err)
	}
	query := parsed.Query()
	query.Set("access_token", token.Value)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (adapter *OfficialHTTPAdapter) doJSON(request *http.Request, target any) error {
	response, err := adapter.client.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return fmt.Errorf("WECHAT_NETWORK: %w", request.Context().Err())
		}
		return errors.New("WECHAT_NETWORK: request failed")
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, maximumTokenResponse+1))
	if err != nil {
		return fmt.Errorf("WECHAT_RESPONSE: %w", err)
	}
	if len(content) > maximumTokenResponse {
		return errors.New("WECHAT_RESPONSE: response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("WECHAT_HTTP: unexpected status %d", response.StatusCode)
	}
	if err := json.Unmarshal(content, target); err != nil {
		return fmt.Errorf("WECHAT_RESPONSE: decode JSON: %w", err)
	}
	return nil
}
