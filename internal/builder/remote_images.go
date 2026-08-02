package builder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

var remoteMarkdownImage = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^\s)]+)\)`)

var blockedRemotePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

const (
	maximumRemoteImageBytes  = 10 << 20
	maximumRemoteImagePixels = 40_000_000
	maximumRemoteRedirects   = 5
)

type hostResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type secureRemoteImageSource struct {
	resolver hostResolver
	dialer   *net.Dialer
}

func newSecureRemoteImageSource() RemoteImageSource {
	return &secureRemoteImageSource{
		resolver: net.DefaultResolver,
		dialer:   &net.Dialer{Timeout: 5 * time.Second},
	}
}

func (source *secureRemoteImageSource) Fetch(ctx context.Context, rawURL string) (RemoteImage, error) {
	currentURL := rawURL
	for redirects := 0; redirects <= maximumRemoteRedirects; redirects++ {
		parsed, addresses, err := source.validateURL(ctx, currentURL)
		if err != nil {
			return RemoteImage{}, err
		}
		response, err := source.fetchOnce(ctx, parsed, addresses)
		if err != nil {
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_FETCH: %w", err)
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			_ = response.Body.Close()
			if redirects == maximumRemoteRedirects {
				return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_REDIRECT: more than %d redirects", maximumRemoteRedirects)
			}
			location, err := response.Location()
			if err != nil {
				return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_REDIRECT: %w", err)
			}
			currentURL = parsed.ResolveReference(location).String()
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_HTTP: unexpected status %d", response.StatusCode)
		}
		content, err := io.ReadAll(io.LimitReader(response.Body, maximumRemoteImageBytes+1))
		closeErr := response.Body.Close()
		if err != nil {
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_READ: %w", err)
		}
		if closeErr != nil {
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_READ: %w", closeErr)
		}
		if len(content) > maximumRemoteImageBytes {
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_SIZE: image exceeds %d bytes", maximumRemoteImageBytes)
		}
		mediaType := strings.Split(http.DetectContentType(content), ";")[0]
		if !supportedRemoteImageType(mediaType) {
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_MIME: unsupported content type %q", mediaType)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil {
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_DECODE: %w", err)
		}
		if config.Width <= 0 || config.Height <= 0 || config.Width > maximumRemoteImagePixels/config.Height {
			return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_PIXELS: image exceeds %d pixels", maximumRemoteImagePixels)
		}
		return RemoteImage{Content: content, MediaType: mediaType}, nil
	}
	return RemoteImage{}, fmt.Errorf("REMOTE_IMAGE_REDIRECT: more than %d redirects", maximumRemoteRedirects)
}

func (source *secureRemoteImageSource) validateURL(ctx context.Context, rawURL string) (*url.URL, []net.IP, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("REMOTE_IMAGE_URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, nil, fmt.Errorf("REMOTE_IMAGE_URL: only HTTP and HTTPS are allowed")
	}
	if parsed.User != nil {
		return nil, nil, fmt.Errorf("REMOTE_IMAGE_URL: user information is not allowed")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return nil, nil, fmt.Errorf("REMOTE_IMAGE_URL: host is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, nil, fmt.Errorf("REMOTE_IMAGE_SSRF: blocked remote image host %q", host)
	}

	addresses := make([]net.IP, 0, 2)
	if literal := net.ParseIP(host); literal != nil {
		addresses = append(addresses, literal)
	} else {
		resolved, err := source.resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, nil, fmt.Errorf("REMOTE_IMAGE_DNS: resolve %q: %w", host, err)
		}
		for _, address := range resolved {
			addresses = append(addresses, address.IP)
		}
	}
	if len(addresses) == 0 {
		return nil, nil, fmt.Errorf("REMOTE_IMAGE_DNS: host %q has no addresses", host)
	}
	for _, address := range addresses {
		if unsafeRemoteAddress(address) {
			return nil, nil, fmt.Errorf("REMOTE_IMAGE_SSRF: blocked remote image host %q", host)
		}
	}
	return parsed, addresses, nil
}

func unsafeRemoteAddress(address net.IP) bool {
	parsed, valid := netip.AddrFromSlice(address)
	if !valid {
		return true
	}
	parsed = parsed.Unmap()
	if !parsed.IsGlobalUnicast() || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsMulticast() || parsed.IsUnspecified() {
		return true
	}
	for _, prefix := range blockedRemotePrefixes {
		if prefix.Contains(parsed) {
			return true
		}
	}
	return false
}

func (source *secureRemoteImageSource) fetchOnce(ctx context.Context, parsed *url.URL, addresses []net.IP) (*http.Response, error) {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, approved := range addresses {
				connection, err := source.dialer.DialContext(ctx, network, net.JoinHostPort(approved.String(), port))
				if err == nil {
					return connection, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname()},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "WeChatLoom/0.3")
	return client.Do(request)
}

func supportedRemoteImageType(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func resolveRemoteImages(ctx context.Context, body []byte, source RemoteImageSource) richMediaResult {
	markdown := string(body)
	assets := map[string][]byte{}
	diagnostics := make([]Diagnostic, 0)
	if source == nil {
		return richMediaResult{Markdown: body, Assets: assets, Diagnostics: diagnostics}
	}

	replacements := map[string]string{}
	for _, match := range remoteMarkdownImage.FindAllStringSubmatch(markdown, -1) {
		reference := match[1]
		if _, processed := replacements[reference]; processed {
			continue
		}
		image, err := source.Fetch(ctx, reference)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "REMOTE_IMAGE_FAILED", Message: fmt.Sprintf("download remote image %q: %v", reference, err), Line: lineOfReference(markdown, reference),
			})
			continue
		}
		mediaType := strings.TrimSpace(strings.Split(image.MediaType, ";")[0])
		if mediaType == "" {
			mediaType = strings.Split(http.DetectContentType(image.Content), ";")[0]
		}
		extension := map[string]string{
			"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
		}[mediaType]
		if extension == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "REMOTE_IMAGE_INVALID", Message: fmt.Sprintf("remote image %q has unsupported content type %q", reference, mediaType), Line: lineOfReference(markdown, reference),
			})
			continue
		}
		sum := sha256.Sum256(image.Content)
		assetPath := fmt.Sprintf("assets/image-%x%s", sum[:8], extension)
		assets[assetPath] = image.Content
		replacements[reference] = assetPath
	}
	for reference, assetPath := range replacements {
		markdown = strings.ReplaceAll(markdown, reference, assetPath)
	}
	return richMediaResult{Markdown: []byte(markdown), Assets: assets, Diagnostics: diagnostics}
}
