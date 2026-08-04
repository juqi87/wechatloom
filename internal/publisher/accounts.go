package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	defaultWeChatAPIBaseURL = "https://api.weixin.qq.com"
	maximumTokenResponse    = 1 << 20
)

type VerifyAccountRequest struct {
	ConfigPath string
	Account    string
}

type AccountReadiness struct {
	Account        string `json:"account"`
	MaskedAppID    string `json:"masked_app_id"`
	Ready          bool   `json:"ready"`
	TokenExpiresIn int    `json:"token_expires_in"`
}

type AccountCredentials struct {
	AppID     string `json:"-" yaml:"app_id"`
	AppSecret string `json:"-" yaml:"app_secret"`
}

type Token struct {
	Value     string
	ExpiresIn int
}

type Service struct {
	port WeChatPort
}

func NewService(port WeChatPort) *Service {
	return &Service{port: port}
}

func NewOfficial() *Service {
	baseURL := strings.TrimSpace(os.Getenv("WECHATLOOM_WECHAT_API_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultWeChatAPIBaseURL
	}
	return &Service{port: &OfficialHTTPAdapter{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}}
}

func (service *Service) VerifyAccount(ctx context.Context, request VerifyAccountRequest) (AccountReadiness, error) {
	configPath, err := resolveUserConfigPath(request.ConfigPath)
	if err != nil {
		return AccountReadiness{}, err
	}
	config, err := loadUserConfig(configPath)
	if err != nil {
		return AccountReadiness{}, err
	}
	accountName := strings.TrimSpace(request.Account)
	if accountName == "" {
		accountName = strings.TrimSpace(config.WeChat.DefaultAccount)
	}
	if accountName == "" {
		return AccountReadiness{}, errors.New("ACCOUNT_NOT_SELECTED: account name or wechat.default_account is required")
	}
	credentials, ok := config.WeChat.Accounts[accountName]
	if !ok {
		return AccountReadiness{}, fmt.Errorf("ACCOUNT_NOT_FOUND: account %q is not configured", accountName)
	}
	credentials.AppID = strings.TrimSpace(credentials.AppID)
	credentials.AppSecret = strings.TrimSpace(credentials.AppSecret)
	if credentials.AppID == "" || credentials.AppSecret == "" {
		return AccountReadiness{}, fmt.Errorf("ACCOUNT_CREDENTIALS_INVALID: account %q requires app_id and app_secret", accountName)
	}
	token, err := service.port.AccessToken(ctx, credentials)
	if err != nil {
		return AccountReadiness{}, err
	}
	return AccountReadiness{
		Account: accountName, MaskedAppID: maskAppID(credentials.AppID), Ready: true, TokenExpiresIn: token.ExpiresIn,
	}, nil
}

type userConfig struct {
	SchemaVersion string       `yaml:"schema_version"`
	WeChat        weChatConfig `yaml:"wechat"`
}

type weChatConfig struct {
	DefaultAccount string                        `yaml:"default_account"`
	Accounts       map[string]AccountCredentials `yaml:"accounts"`
}

func resolveUserConfigPath(explicit string) (string, error) {
	selected := strings.TrimSpace(explicit)
	if selected == "" {
		configDirectory, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		selected = filepath.Join(configDirectory, "wechatloom", "config.yaml")
	}
	return filepath.Abs(selected)
}

func loadUserConfig(path string) (userConfig, error) {
	info, err := os.Stat(path)
	if err != nil {
		return userConfig{}, fmt.Errorf("open user config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return userConfig{}, errors.New("USER_CONFIG_INVALID: user config must be a regular file")
	}
	if err := validateUserConfigPermissions(path, info); err != nil {
		return userConfig{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return userConfig{}, fmt.Errorf("open user config: %w", err)
	}
	defer file.Close()
	var config userConfig
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return userConfig{}, fmt.Errorf("decode user config: %w", err)
	}
	if config.SchemaVersion != "1" {
		return userConfig{}, errors.New(`USER_CONFIG_SCHEMA: schema_version must be "1"`)
	}
	return config, nil
}

type OfficialHTTPAdapter struct {
	baseURL string
	client  *http.Client
}

func (adapter *OfficialHTTPAdapter) AccessToken(ctx context.Context, account AccountCredentials) (Token, error) {
	endpoint, err := url.Parse(strings.TrimRight(adapter.baseURL, "/") + "/cgi-bin/token")
	if err != nil {
		return Token{}, fmt.Errorf("WECHAT_TOKEN_ENDPOINT: %w", err)
	}
	query := endpoint.Query()
	query.Set("grant_type", "client_credential")
	query.Set("appid", account.AppID)
	query.Set("secret", account.AppSecret)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Token{}, fmt.Errorf("WECHAT_TOKEN_REQUEST: %w", err)
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Token{}, fmt.Errorf("WECHAT_TOKEN_NETWORK: %w", ctx.Err())
		}
		return Token{}, errors.New("WECHAT_TOKEN_NETWORK: request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumTokenResponse+1))
	if err != nil {
		return Token{}, fmt.Errorf("WECHAT_TOKEN_RESPONSE: %w", err)
	}
	if len(body) > maximumTokenResponse {
		return Token{}, errors.New("WECHAT_TOKEN_RESPONSE: response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Token{}, fmt.Errorf("WECHAT_TOKEN_HTTP: unexpected status %d", response.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrorCode   int    `json:"errcode"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Token{}, fmt.Errorf("WECHAT_TOKEN_RESPONSE: decode JSON: %w", err)
	}
	if payload.ErrorCode != 0 {
		return Token{}, classifyWeChatError(payload.ErrorCode)
	}
	if strings.TrimSpace(payload.AccessToken) == "" || payload.ExpiresIn <= 0 {
		return Token{}, errors.New("WECHAT_TOKEN_RESPONSE: access_token and expires_in are required")
	}
	return Token{Value: payload.AccessToken, ExpiresIn: payload.ExpiresIn}, nil
}

func classifyWeChatError(code int) error {
	switch code {
	case 40164:
		return errors.New("WECHAT_IP_NOT_ALLOWED: add the current public IP to the WeChat API allowlist")
	case 40013, 40125:
		return errors.New("WECHAT_AUTH_FAILED: account credentials were rejected")
	default:
		return fmt.Errorf("WECHAT_API_ERROR: WeChat returned error code %d", code)
	}
}

func maskAppID(appID string) string {
	runes := []rune(appID)
	if len(runes) <= 8 {
		return "****"
	}
	return string(runes[:4]) + "…" + string(runes[len(runes)-4:])
}
