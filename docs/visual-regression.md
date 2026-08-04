# 视觉回归

仓库把组件画廊的 320、375、430 px 截图保存在 `testdata/visual/tech-cyan/`。测试不会自动修改这些基线。

## 检查

确保本机安装 Chrome、Chromium 或 Edge，或显式设置浏览器：

```bash
export WECHATLOOM_BROWSER=/path/to/chrome
WECHATLOOM_VISUAL_REGRESSION=1 go test ./internal/snapshot -run TestComponentGalleryVisualBaseline -count=1
```

测试逐像素比较同尺寸图片，允许至多 0.5% 像素出现可见差异。HTML 结构测试始终运行；浏览器像素测试只在显式启用时运行。

## 更新基线

1. 先运行全量测试。
2. 用 `wechatloom build testdata/component-gallery.md --theme tech-cyan` 构建新画廊。
3. 用 `wechatloom snapshot <build-path> --output <temporary-directory>` 生成候选图。
4. 肉眼检查三个宽度，尤其检查标题断行、表格、长链接、图片、代码和双栏组件。
5. 只有视觉变化符合设计意图时，才显式替换 `testdata/visual/tech-cyan/`。
6. 再次运行像素回归并在变更说明中记录原因。

CI 使用固定 Chrome for Testing 150 的 `chrome-headless-shell` 生成 Linux smoke 截图并上传为 artifact。专用无头二进制避免完整版 Chrome 在无桌面 Runner 中启动 DBus、UPower 和后台注册服务；它用于跨平台检查，不自动覆盖 macOS golden。
