package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"

	"github.com/wechatloom/wechatloom/internal/catalog"
)

func renderPreviewPage(ctx context.Context, resourceCatalog catalog.Catalog, title string, body []byte, selectedTheme string) ([]byte, error) {
	listed, err := resourceCatalog.List(ctx, catalog.Query{Kind: catalog.KindTheme})
	if err != nil {
		return nil, err
	}
	articles := make(map[string]string, len(listed.Resources))
	var options bytes.Buffer
	for _, resource := range listed.Resources {
		definition, err := resourceCatalog.Show(ctx, catalog.Ref{Kind: catalog.KindTheme, Name: resource.Name})
		if err != nil {
			return nil, err
		}
		rendered, _, err := renderArticle(body, *definition.Theme)
		if err != nil {
			return nil, err
		}
		articles[resource.Name] = string(rendered)
		selected := ""
		if resource.Name == selectedTheme {
			selected = " selected"
		}
		fmt.Fprintf(&options, "<option value=\"%s\"%s>%s · %s</option>\n", html.EscapeString(resource.Name), selected, html.EscapeString(resource.Family), html.EscapeString(resource.Name))
	}
	encodedArticles, err := json.Marshal(articles)
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	fmt.Fprintf(&output, `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self' data: blob: file:; style-src 'unsafe-inline'; script-src 'unsafe-inline'">
<title>%s · WeChatLoom Preview</title>
<style>
:root{color-scheme:light;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC",sans-serif}
*{box-sizing:border-box}body{background:#eef1f5;color:#111827;margin:0}.toolbar{align-items:center;background:#111827;color:#fff;display:flex;flex-wrap:wrap;gap:12px;padding:12px 18px;position:sticky;top:0;z-index:10}.brand{font-weight:800;margin-right:auto}.control{align-items:center;display:flex;font-size:13px;gap:6px}.control select,.control button{background:#fff;border:0;border-radius:6px;color:#111827;cursor:pointer;padding:7px 10px}.control button[aria-pressed="true"]{background:#22d3ee;color:#083344;font-weight:800}.notice{background:#fffbeb;border-bottom:1px solid #fde68a;color:#78350f;font-size:13px;padding:9px 18px;text-align:center}.stage{align-items:flex-start;display:flex;justify-content:center;min-height:calc(100vh - 98px);overflow:auto;padding:28px}.device{background:#fff;border:1px solid #cbd5e1;border-radius:20px;box-shadow:0 20px 60px rgba(15,23,42,.16);overflow:hidden;padding:12px;transition:width .2s;width:375px}.speaker{background:#d1d5db;border-radius:4px;height:5px;margin:0 auto 10px;width:52px}iframe{background:#fff;border:0;display:block;min-height:740px;width:100%%}@media(max-width:520px){.toolbar{position:static}.stage{padding:12px}.device{border-radius:12px;max-width:100%%}}
</style>
</head>
<body>
<nav class="toolbar" aria-label="预览控制">
<span class="brand">WeChatLoom Preview</span>
<label class="control">主题 <select data-preview-control="theme" aria-label="切换主题">
%s</select></label>
<div class="control" aria-label="切换手机宽度">
<button type="button" data-preview-width="320">320</button>
<button type="button" data-preview-width="375" aria-pressed="true">375</button>
<button type="button" data-preview-width="430">430</button>
</div>
</nav>
<div class="notice" role="note">本地预览不会写入微信；最终视觉以微信公众号草稿箱渲染为准。</div>
<main class="stage"><div class="device" data-preview-frame><div class="speaker"></div><iframe title="%s"></iframe></div></main>
<script>
const articles=%s;
const theme=document.querySelector('[data-preview-control="theme"]');
const frame=document.querySelector('[data-preview-frame]');
const iframe=frame.querySelector('iframe');
function render(){iframe.srcdoc='<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;padding:18px}</style>'+articles[theme.value]}
theme.addEventListener('change',render);
document.querySelectorAll('[data-preview-width]').forEach(button=>button.addEventListener('click',()=>{frame.style.width=button.dataset.previewWidth+'px';document.querySelectorAll('[data-preview-width]').forEach(item=>item.setAttribute('aria-pressed',String(item===button)))}));
render();
</script>
</body>
</html>
`, html.EscapeString(title), options.String(), html.EscapeString(title), encodedArticles)
	return output.Bytes(), nil
}
