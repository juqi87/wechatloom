# md2wechat 产品能力调研

> 调研日期：2026-07-28
> 调研范围：md2wechat 官方站点、官方文档、官方 OpenAPI、官方 `md2wechat-skill` 与 `md2wechat-lite` 仓库。
> 证据原则：事实优先引用官方一手来源；未被官方明确承诺的结论标为“判断”或“待验证”。

## 一、结论先行

1. md2wechat 不是单一的 Markdown 编辑器，而是一套由**网页编辑器、Agent API、完整 CLI、Skill 操作协议和轻量 CLI**组成的产品体系。官网把 `md2wechat-lite` 定义为 CLI 层，把 `md2wechat-skill` 定义为 Skill 层，同时允许直接调用 Raw API。[官网产品页](https://www.md2wechat.com/)｜[官方 Skills 文档](https://www.md2wechat.com/docs/skills)
2. 官网当前公开的 Agent API 只有 4 个端点：Markdown 转 HTML、文章草稿、新图文草稿、远程图片批量上传。[API Overview](https://www.md2wechat.com/docs/api)｜[OpenAPI JSON](https://www.md2wechat.com/openapi.v1.json)
3. 完整 `md2wechat` CLI 的能力远多于公开 Agent API：除转换和草稿外，还有文章检查、预览、增强建议、写作、去 AI 痕迹、标题建议、封面与信息图生成、图片上传、多账号、主题/布局/Prompt/Provider 发现、配置诊断等。[官方完整仓库 README](https://github.com/geekjourneyx/md2wechat-skill#readme)｜[CLI 使用文档](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md)
4. **公开证据只确认“写入微信公众号草稿箱/创建图片帖子草稿”，没有确认“正式发布、定时发布或群发”。** 官网 4 个端点没有正式发布端点，完整 CLI 的推荐发布链也停在 `convert --draft` / `create_image_post`。因此，不能把产品中的 “publishing” 宣传用词直接理解成“文章已正式对粉丝发布”。[OpenAPI JSON](https://www.md2wechat.com/openapi.v1.json)｜[官方 Skill 的 Publishing Side Effects](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#publishing-side-effects)
5. 主题、布局模块和价格信息存在明显的版本漂移：官网主题页写 48 个公开主题，OpenAPI 的 `theme` 枚举只有 5 个，轻量 CLI README 写 38+；完整仓库 README 又写 48 个专业主题。运行时应把 CLI discovery 或服务端最新目录作为真相源，而不是把某个固定数字硬编码进 Agent。[主题目录](https://www.md2wechat.com/themes)｜[OpenAPI JSON](https://www.md2wechat.com/openapi.v1.json)｜[lite README](https://github.com/geekjourneyx/md2wechat-lite#readme)｜[Discovery 文档](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/DISCOVERY.md)
6. 两个官方仓库的许可不能混用：完整 `md2wechat-skill` 当前是带商业用途限制的 Source Available License；`md2wechat-lite` 是 MIT License。[完整仓库 LICENSE](https://github.com/geekjourneyx/md2wechat-skill/blob/main/LICENSE)｜[lite LICENSE](https://github.com/geekjourneyx/md2wechat-lite/blob/main/LICENSE)

## 二、产品组成与职责边界

| 产品面 | 已证实定位 | 主要入口 |
|---|---|---|
| 官网与文档 | API onboarding、功能说明、主题目录、示例、定价、OpenAPI 与 `llms.txt` | [md2wechat.com](https://www.md2wechat.com/) |
| 网页编辑器 | 官网把 `md2wechat.cn` 列为 Editor，把 `md2wechat.app` 列为备用域名；主题页可跳转到编辑器做更深入的视觉查看 | [Docs 首页](https://www.md2wechat.com/docs)｜[主题目录](https://www.md2wechat.com/themes) |
| Agent API | 面向能发送 HTTP 请求的 Agent，负责转换、草稿和素材上传 | [API 文档](https://www.md2wechat.com/docs/api) |
| 完整 CLI：`md2wechat` | 本地编排与 discovery 层，覆盖内容生产、检查、排版、图片、微信草稿和多账号 | [完整仓库](https://github.com/geekjourneyx/md2wechat-skill) |
| Skill：`md2wechat` | 不是渲染器本体，而是指导 Agent 如何安全、可预测地调用 CLI 的 SOP | [SKILL.md](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md) |
| 轻量 CLI：`md2wx` | 较小的 Go CLI，直接包装转换、文章草稿、新图文草稿和批量上传 | [lite 仓库](https://github.com/geekjourneyx/md2wechat-lite) |

### 重要架构判断

**已证实：** Skill 明确假设 `md2wechat` 已在 `PATH`，其职责是意图路由、能力发现、发布前检查和副作用约束；真正执行转换、上传、建草稿的是 CLI/API。[官方 Skill](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#configuration-boundaries)

**判断：** md2wechat 的核心产品模式是“**确定性工具提供能力，Skill 让 Agent 正确编排能力**”，而不是把所有实现都写进 `SKILL.md`。

## 三、官网公开 Agent API

截至调研日，官网 API Overview 和 OpenAPI 均只列出以下 4 个 `v1` 端点。[API Overview](https://www.md2wechat.com/docs/api)｜[OpenAPI JSON](https://www.md2wechat.com/openapi.v1.json)

| 端点 | 输入 | 鉴权 | 已证实结果 |
|---|---|---|---|
| `POST /api/v1/convert` | 必填 `markdown`；可选 `theme`、`fontSize`、`convertVersion` | `Md2wechat-API-Key` | 微信可用 HTML |
| `POST /api/v1/article-draft` | 必填 `markdown`；可选主题、字号、转换版本、`coverImageUrl` | md2wechat API Key + 微信 AppID/AppSecret | 微信文章草稿结果 |
| `POST /api/v1/newspic-draft` | 必填 `title`、`content`、`imageUrls[]` | md2wechat API Key + 微信 AppID/AppSecret | 新图文/图片文章草稿结果 |
| `POST /api/v1/batch-upload` | 必填 `imageUrls[]` | md2wechat API Key + 微信 AppID/AppSecret | 微信素材上传结果 |

补充事实：

- `convert` 只需 md2wechat API Key；草稿和素材端点还要求 `Wechat-Appid` 与 `Wechat-App-Secret`。[Auth](https://www.md2wechat.com/docs/auth)
- 官方要求 Secret 留在服务端，不要暴露给浏览器客户端，并建议草稿/素材请求使用服务端 `curl` 或 fetch。[Auth](https://www.md2wechat.com/docs/auth)
- `newspic-draft` 面向 title、content 和多张图片 URL 组成的图片型内容；官方建议需要更严格控制素材时先预上传图片。[Newspic Draft](https://www.md2wechat.com/docs/api/newspic-draft)
- `batch-upload` 下载远程图片并返回微信素材结果，适合作为文章或新图文创建前的素材预处理。[Batch Upload](https://www.md2wechat.com/docs/api/batch-upload)

### API 文档未承诺的部分

公开 OpenAPI 对四个端点都只描述了 `200`，没有给出完整响应 schema、错误 schema、速率限制、幂等键、超时、重试策略或版本兼容承诺。[OpenAPI JSON](https://www.md2wechat.com/openapi.v1.json)

这意味着仅根据公开 OpenAPI，Agent 不能可靠推导：

- 草稿返回对象的稳定字段；
- 图片批量上传的部分成功语义；
- 同一请求重试是否会创建重复草稿；
- 认证失败、微信失败与服务端失败的 HTTP 状态映射。

## 四、完整 CLI 的用户流程

### 4.1 文章主流程

官方 Skill 推荐 confirm-first：

1. `md2wechat inspect article.md --json`
2. `md2wechat preview article.md`
3. `md2wechat convert article.md ...`
4. 只有用户明确要求远程副作用时，才添加 `--upload`、`--draft`、`--cover` 或 `--cover-media-id`

来源：[官方 Skill：Article Workflow](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#article-workflow)

各阶段边界：

- `inspect` 输出最终 metadata、检查项、目标 readiness 和 blockers；`data.readiness.targets` / `data.readiness.blockers` 是文章级发布门禁。[USAGE](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#确认层命令)
- `preview` 只在 API 转换成功时写本地静态 HTML，不上传图片、不创建草稿、不写回 Markdown。[USAGE](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#确认层命令)
- 普通 `convert` 只转换；图片上传和 URL 替换仅发生在显式 `--upload` 或 `--draft` 路径。[USAGE](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#确认层命令)
- 建草稿必须显式提供本地封面 `--cover` 或已有永久素材 `--cover-media-id`，两者互斥。[USAGE：草稿管理](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#草稿管理)
- 可用 `--save-draft draft.json` 只保存草稿 JSON、不提交微信，也可以随后用 `create_draft draft.json` 创建草稿。[USAGE：草稿管理](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#草稿管理)

### 4.2 图片帖子流程

图片型帖子不是文章草稿的一个参数变体。官方 Skill 要求对 image-first post、image note、newspic、多图帖子使用 `create_image_post`，不要用 `convert --draft` 代替。[官方 Skill：Intent Routing](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#intent-routing)

CLI 提供：

```bash
md2wechat create_image_post --title "Weekend Trip" --images a.jpg,b.jpg
md2wechat create_image_post --title "Weekend Trip" --images a.jpg,b.jpg --dry-run --json
```

来源：[QUICKSTART](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/QUICKSTART.md)

### 4.3 元数据处理

完整 CLI 的公开规则为：

- 标题：`--title` → frontmatter `title` → 正文首个 Markdown 标题 → `未命名文章`
- 作者：`--author` → frontmatter `author`
- 摘要：`--digest` → frontmatter `digest` → `summary` → `description`
- 文档写明长度上限为标题 32、作者 16、摘要 128 个字符；建草稿时摘要为空会从正文 HTML 生成 120 字符兜底摘要

来源：[USAGE：文章元数据规则](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#文章元数据规则)

## 五、完整 CLI 的能力清单

以下能力由官方完整仓库 README、Skill 和使用文档明确列出。

| 能力组 | 命令/能力 | 行为边界 |
|---|---|---|
| 文章确认 | `inspect`、`advise`、`preview` | 检查、建议和本地预览；`advise` 不修改文章 |
| 排版转换 | `convert` | Markdown → 微信 HTML；显式参数才触发上传/草稿 |
| 草稿 | `convert --draft`、`create_draft`、`create_image_post` | 创建文章草稿或图片帖子草稿 |
| 内容生产 | `write`、`humanize` | 从想法起稿；去 AI 痕迹/真人化 |
| 标题 | `title suggest` | 生成交给宿主 Agent/外部模型的请求，不直接调用模型、不写回文章、不建草稿 |
| 图片 | `generate_image`、`generate_cover`、`generate_infographic` | 直接调 provider，或只输出宿主 Agent 图片计划 |
| 微信素材 | `upload_image`、`download_and_upload`、`convert --upload` | 上传本地/远程图片并获得微信素材结果 |
| 资源发现 | `themes`、`layout`、`providers`、`prompts`、`skills` | list/show/render/validate 等机器可读 discovery |
| 系统发现 | `version`、`capabilities`、`doctor` | 版本、能力、配置可尝试性 |
| 配置与账号 | `config init/show/validate/wechat-accounts` | 初始化、查看生效配置、多账号发现 |

来源：[官方 README](https://github.com/geekjourneyx/md2wechat-skill#readme)｜[官方 Skill](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md)｜[USAGE](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md)

## 六、API 模式与 AI 模式

| 维度 | API 模式 | AI 模式 |
|---|---|---|
| 默认性 | CLI 默认模式 | 用户显式请求 |
| 本次 CLI 输出 | 最终 HTML | `action_required` + `data.prompt` |
| HTML 生成方 | md2wechat 排版 API | 宿主 Agent或外部模型 |
| 高级 `:::module` | 支持 | 不解析、不渲染 |
| 本次调用的上传/建草稿 | 按显式参数执行 | 不执行 |
| 凭证 | `MD2WECHAT_API_KEY` | md2wechat 不需要内置模型 Key；外部模型由宿主管理 |

来源：[USAGE：转换模式](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#转换模式)｜[官方 Skill：API And AI Mode](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#api-and-ai-mode)

官方 Skill 明确禁止 API 失败后悄悄降级为 AI 模式，因为这会改变输出能力；只有用户请求或接受失去高级布局渲染时才能使用 AI 模式。[官方 Skill](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#api-and-ai-mode)

## 七、主题与高级布局

### 7.1 主题

官网主题页在页面上标示：

- 48 个公开主题；
- 8 个 collection；
- 页面标注 “Last synced: 2026-03-14”；
- collection 包含 Built-in、Classic、Modern、Extra、Minimal、Focus、Elegant、Bold；
- 主题元数据包含内容场景、密度、对比度与实际 `theme` 参数。

来源：[官方主题目录](https://www.md2wechat.com/themes)

完整 CLI 的 discovery 规则更适合 Agent：

- `themes list --json` 返回 `name`、`type`（`api`/`ai`）、`selectable`、`api_theme`、风格元数据等；
- collection descriptor 也可能出现，但 `selectable:false`，不能直接传给 `convert --theme`；
- API 模式只能选 `type:api` 且 `selectable:true` 的主题，AI 模式同理；
- 主题资产可被项目目录、用户配置目录或 `MD2WECHAT_THEMES_DIR` 覆盖。

来源：[Discovery 文档](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/DISCOVERY.md)｜[官方 Skill：Theme Selection](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#theme-selection)

### 7.2 高级布局

高级布局把结构化模块直接写进 `markdown` 字段，并继续调用同一个 `convert` 端点。[Convert 文档](https://www.md2wechat.com/docs/api/convert)

基础语法为：

```markdown
:::module-name
field: value
:::
```

行式模块用 `|` 分隔列，也可以用 `:::metrics[Core numbers]` 形式携带标题。官方特别指出常见失败包括中文冒号、字段名错误、列缺失、模块名错误和必填字段缺失。[Advanced Layout Syntax](https://www.md2wechat.com/docs/api/advanced-layout-syntax)

公开 recipe 列出的代表性模块包括：

- 发布说明：`hero`、`cards`、`compare`/`steps`、`summary`、`cta`
- 长方法文：`verdict`、`toc`、`part`、`bridge`、`quote`/`summary`、`author-card`
- 教程：`hero`、`steps`、`image-text`/`image-steps`、`notice`、`checklist`
- 服务/转化页：`audience-fit`、`verdict`、`cases`、`pricing`、`faq`、`subscribe`/`cta`
- 品牌系列：`hero`、`manifesto`、`quote`、`series`、`author-card`、`subscribe`

来源：[Advanced Layout Recipes](https://www.md2wechat.com/docs/api/advanced-layout-recipes)

完整仓库在当前 `main` 文档中给出的计数维度是：

- 68 个推荐使用场景条目；
- 53 个推荐 `:::module` 语法名；
- 3 个兼容模块；
- 4 个基础增强能力；
- 合计 60 项渲染层语法能力。

这些不是同一维度，不能把“68 场景”和“53 模块”互换。[官方 README：高级排版](https://github.com/geekjourneyx/md2wechat-skill#高级排版)｜[Discovery 文档：能力总览](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/DISCOVERY.md#能力总览)

官方对 Agent 的推荐是每篇文章优先使用 3–6 个模块、一个屏幕保留一个主判断，并先解决信息结构再追求视觉变化。[Advanced Layout Syntax](https://www.md2wechat.com/docs/api/advanced-layout-syntax)

## 八、AI 写作、标题与品牌风格

### 写作与去 AI 痕迹

- `write` 用于从想法生成文章或按作者风格起稿；
- `humanize` 用于识别工整句式、空泛用词、报告式总结、连续排比和聊天式套话，并按强度重写；
- `humanize` 文档列出多种强度模式并支持 Agent JSON 输出。

来源：[官方 README](https://github.com/geekjourneyx/md2wechat-skill#readme)｜[HUMANIZE 文档](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/HUMANIZE.md)

### 标题建议

`md2wechat title suggest article.md --json` 读取文章并构造标题生成请求，可设置目标读者、候选数量、最大长度和 hook level。它返回 `TITLE_SUGGEST_REQUEST_READY`，由宿主 Agent 或外部模型执行；CLI 自身不调用模型、不写回 Markdown、不创建草稿，也不确认最终标题。[USAGE：标题建议](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#标题建议)

### Brand Profile

长期风格偏好可放在 `~/.config/md2wechat/brand.md`。它是自由格式 Markdown，CLI 不解析，由 Agent 作为语气、主题、模块、CTA 和禁用表达的上下文读取；不存在时不应阻塞任务。[官方 Skill：Brand Profile](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#brand-profile)

## 九、图片能力

### 9.1 文章图片与微信素材

完整 CLI 支持：

- Markdown 本地图片；
- 远程图片，先下载再上传；
- `![alt](__generate:prompt__)` 形式的 AI 图片占位；
- `upload_image` 上传单图；
- `download_and_upload` 下载并上传远程图；
- `convert --upload` 上传正文图片并替换 HTML URL；
- 大图压缩，文档默认上限为宽 1920px、大小 5MB。

来源：[USAGE：图片处理](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#图片处理)｜[CONFIG：图片处理配置](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/CONFIG.md#图片处理配置)

### 9.2 直接图片生成

直接生成入口包括：

- `generate_image`
- `generate_cover`
- `generate_infographic`

文档列出的内置 provider 包括 OpenAI、TuZi、ModelScope、OpenRouter、Gemini 和 Volcengine，并允许用 `--model` 覆盖单次模型。具体模型目录应由 `providers list/show --json` 发现，而非凭记忆写死。[CONFIG：图片生成配置](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/CONFIG.md#图片生成配置)｜[IMAGE_PROVISIONERS](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/IMAGE_PROVISIONERS.md)

### 9.3 宿主 Agent 图片计划

当 Codex 等宿主已有图片生成工具时，可运行：

```bash
md2wechat generate_cover --article article.md --plan --json
md2wechat generate_infographic --article article.md --plan --json
```

返回 `IMAGE_PLAN_READY`，包含 prompt、用途和画幅；该步骤不请求图片 provider、不需要 `IMAGE_API_KEY`、不上传微信，执行所有者是宿主 Agent。宿主完成图片生成并保存本地文件后，再调用 `upload_image` 或把本地文件作为 `--cover` 使用。[AGENT_IMAGE_GEN](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/AGENT_IMAGE_GEN.md)

## 十、配置、账号与外部依赖

### 10.1 配置来源

- 默认配置：`~/.config/md2wechat/config.yaml`
- 优先级：命令行参数 > 环境变量 > 配置文件 > 默认值
- 可用 `config init`、`config show --format json`、`config validate` 管理与诊断

来源：[CONFIG](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/CONFIG.md)

### 10.2 最小凭证

| 工作流 | 需要 |
|---|---|
| API 模式转换/预览 | `MD2WECHAT_API_KEY` 或 `api.md2wechat_key` |
| 微信图片上传/文章草稿/图片帖子草稿 | 微信 AppID + AppSecret；相关高级路径还需有效 md2wechat API Key |
| CLI 直接生成图片 | 图片 provider、API Key、base URL/模型等 |
| 宿主 Agent 图片计划 | 不需要 CLI 图片 provider Key，但宿主必须实际拥有 Image Gen 能力 |

来源：[Auth](https://www.md2wechat.com/docs/auth)｜[CONFIG](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/CONFIG.md)｜[官方 Skill：Configuration Boundaries](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#configuration-boundaries)

### 10.3 多公众号

完整 CLI 支持在同一配置中设置 `wechat.default_account` 与多个命名账号；执行时按 `--wechat-account`、`WECHAT_ACCOUNT`、默认账号、直接凭证、唯一命名账号的顺序选择。命名账号执行远程微信副作用前会校验 md2wechat API Key；`config wechat-accounts --json` 是本地只读且不输出 Secret。[CONFIG：多公众号配置](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/CONFIG.md#多公众号配置)

### 10.4 微信 IP 白名单

官方凭证指南指出，上传和建草稿等微信 API 请求可能要求执行机器公网 IP 位于公众号后台白名单。完整 CLI 还提供高级服务的 `wechat.proxy_url` / `WECHAT_PROXY_URL` 固定出口能力，该代理只作用于微信上传、草稿和图片消息副作用。[WECHAT-CREDENTIALS](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/WECHAT-CREDENTIALS.md)｜[CONFIG：微信配置](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/CONFIG.md#微信配置)

## 十一、错误处理与安全门禁

### 官网 Agent API 的公开错误建议

官网列出的常见错误只有：

- 缺少 md2wechat API Key；
- 缺少微信 AppID/AppSecret；
- 远程图片 URL 无效；
- Markdown payload 无效。

官方建议发送前检查 headers，只重试网络错误、不重试认证错误，日志只记录 payload 形状而不记录 Secret，并在错误摘要中带上 endpoint 名称。[Errors](https://www.md2wechat.com/docs/errors)

### 完整 CLI 的防误操作机制

- `doctor --json` 是本地只读检查，不访问远程 API、不做 live auth、不上传、不建草稿；它只回答配置是否具备“可尝试性”。[Discovery：本地体检](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/DISCOVERY.md#本地体检)
- `inspect --json` 是单篇文章的目标 readiness，Agent 应依据 `targets` 和 `blockers` 判断能否转换、上传或建草稿。[官方 Skill：Article Workflow](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#article-workflow)
- 布局错误应先 `layout validate`，再用 `layout show` 核对具体模块；未知模块只 warning 以便前向兼容，但拼写仍应与 discovery 核对。[官方 Skill：Failure Handling](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md#failure-handling)
- 草稿错误 `45004` 应优先检查 digest/summary/description，不应先假设正文过长；文档还列出未认证公众号、敏感词、调用次数上限等可能原因。[Troubleshooting：草稿创建失败](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/TROUBLESHOOTING.md#草稿创建失败)
- 完整 CLI 的 JSON envelope 公开示例含 `success`、`code`、`message`、`schema_version`、`status`、`retryable` 和 `data`，适合 Agent 分支处理。[USAGE：AI 生成图片](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md#ai-生成图片)

## 十二、“草稿”与“正式发布/群发”的边界

### 已证实

- 公开 Agent API 只有 `convert`、`article-draft`、`newspic-draft`、`batch-upload`。[OpenAPI JSON](https://www.md2wechat.com/openapi.v1.json)
- 官网页面把 Article Draft 描述为“ready-to-review drafts”，即待审核草稿。[官网产品页](https://www.md2wechat.com/)
- 完整 CLI 的文章远程动作是图片上传与草稿创建，图片型内容使用 `create_image_post`；官方使用示例都以“上传到草稿箱”为终点。[USAGE](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md)｜[官方 Skill](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md)

### 判断

截至 2026-07-28，公开官网、OpenAPI、完整 CLI README、Skill 和使用文档中，**没有可确认的正式发布、定时发布或群发命令/端点**。也没有公开文档展示微信 `freepublish` 或群发接口的调用链。

因此，对本次产品分析应采用以下边界：

- “发布到草稿箱”：md2wechat 已明确支持；
- “生成发布准备结果”：md2wechat 已明确支持；
- “正式发布到公众号主页”：公开材料未证实；
- “群发给关注者”：公开材料未证实；
- “定时发布”：公开材料未证实。

官网中的 “publishing API/workflow” 是上位概念，不能单凭该措辞推断已经执行正式发布。

### 微信原生接口提供的进一步可能性

微信官方文档把“草稿箱”和“发布能力”定义为两组独立接口：

- 草稿箱包括 `/cgi-bin/draft/add`、`draft/update`、`draft/get`、`draft/delete` 等接口；
- 发布能力包括 `/cgi-bin/freepublish/submit`、`freepublish/get`、`freepublish/getarticle`、`freepublish/delete` 和 `freepublish/batchget`；
- `freepublish/submit` 的官方描述是“将图文草稿提交发布”。

来源：[微信官方草稿箱文档](https://developers.weixin.qq.com/doc/offiaccount/Draft_Box/Add_draft.html)｜[微信官方发布能力文档](https://developers.weixin.qq.com/doc/offiaccount/Publish/Publish.html)

微信官方发布能力页面同时注明：该组服务端接口面向服务号；自 2025 年 7 月起，个人主体账号、企业主体未认证账号及不支持认证的账号会被回收相关接口调用权限。

**对本产品的判断：** 技术上可以在 md2wechat 式草稿工作流之后另接微信原生 `freepublish` 发布步骤，但必须将它设计成独立、高风险、按账号能力探测的动作，不能假定所有公众号都具备正式发布权限，也不能把“创建草稿成功”等同于“正式发布成功”。

## 十三、轻量版 `md2wechat-lite`

`md2wechat-lite` 使用 `md2wx` 命令，公开能力更接近官网 4 个 API：

- `article-draft`：Markdown 转排版并创建图文草稿；
- `newspic-draft`：标题、内容和多图创建图片草稿；
- `batch-upload`：上传远程图片；
- `themes list`：列主题；
- `config set/list`：配置微信凭证、API Key、主题和字号。

它的 `--cover-image`、`newspic-draft --images`、`batch-upload --images` 只支持公网 URL，不支持本地路径或通配符。配置优先级同样是命令行 > 环境变量 > 配置文件 > 默认值。[lite README](https://github.com/geekjourneyx/md2wechat-lite#readme)

## 十四、许可差异

### 完整仓库 `md2wechat-skill`

当前主分支采用 **md2wechat Source Available License**，基于 Business Source License 1.1。许可证明确把 CLI 源码、主题、布局模块、Prompt 模板、Skill 定义、文档与资产都纳入 Licensed Work；个人非商业内容创作、学习研究、评估、非营利和贡献允许免费使用，但商业产品/服务、营利组织内部商业流程、付费客户交付、白标/重新分发和商业 AI 训练等需要书面商业授权。Change Date 标为 2030-01-01，之后转 Apache-2.0；历史 MIT 版本不受新许可证影响。[完整仓库 LICENSE](https://github.com/geekjourneyx/md2wechat-skill/blob/main/LICENSE)

### 轻量仓库 `md2wechat-lite`

当前主分支采用标准 MIT License，允许使用、复制、修改、合并、发布、分发、再许可和销售，但须保留版权与许可声明。[lite LICENSE](https://github.com/geekjourneyx/md2wechat-lite/blob/main/LICENSE)

### 对“借鉴 md2wechat”的含义

**判断：** 可以借鉴公开的产品思路、工作流分层和通用接口概念；若直接复制完整仓库的主题、布局模块、Prompt、Skill 内容或代码用于商业产品，需先核对并遵守其 Source Available License，不能因 `md2wechat-lite` 是 MIT 就推定完整仓库也可自由商用。

## 十五、版本漂移与公开资料不一致

以下差异在调研日同时存在：

| 项目 | 来源 A | 来源 B | 建议解释 |
|---|---|---|---|
| 主题数量 | 官网主题页：48，标注 2026-03-14 同步 | lite README：38+；OpenAPI enum：5 | 不同产品层/版本；运行时 discovery 为准 |
| 高级布局数量 | 完整 README：68 场景、53 推荐语法、60 渲染能力 | 官网 recipe 只列代表性模块 | 数字维度不同；不硬编码 |
| 价格 | 官网首页 Unified 标 ¥199 | `/docs/pricing` 标 ¥129 | 公开页面未同步；购买前人工确认 |
| 完整 CLI 版本 | `main` 的部分文档引用 v3.1.0/v3.2.0 | GitHub latest release 页面在调研时显示 v2.9.0 | 主分支文档可能领先 release；以实际二进制 `version --json` 为准 |

来源：[主题目录](https://www.md2wechat.com/themes)｜[OpenAPI](https://www.md2wechat.com/openapi.v1.json)｜[lite README](https://github.com/geekjourneyx/md2wechat-lite#readme)｜[完整 README](https://github.com/geekjourneyx/md2wechat-skill#readme)｜[官网首页](https://www.md2wechat.com/)｜[Pricing 文档](https://www.md2wechat.com/docs/pricing)｜[GitHub Releases](https://github.com/geekjourneyx/md2wechat-skill/releases/latest)

## 十六、仍需实测或向作者确认的事项

以下内容不能从公开一手资料可靠回答：

1. 4 个公开 Agent API 的稳定响应字段、错误码表、限流和幂等语义；
2. 草稿端点在重复请求、部分图片上传失败时的事务边界；
3. 48 个主题是否全部能被当前生产 API 接受，尤其 OpenAPI enum 只列 5 个；
4. 新图文草稿在微信后台的最终对象类型与所有账号权限要求；
5. 是否存在未公开的正式发布、定时发布或群发能力；
6. 官网当前实际成交价格；
7. 高级固定出口代理的 SLA、地域、数据处理与安全边界；
8. 网页编辑器的完整交互功能，因为官网公开文档主要描述 Agent API/CLI，未提供与 CLI 等粒度的编辑器功能清单。

## 十七、官方来源索引

- [官网](https://www.md2wechat.com/)
- [Docs 首页](https://www.md2wechat.com/docs)
- [Quickstart](https://www.md2wechat.com/docs/quickstart)
- [Auth](https://www.md2wechat.com/docs/auth)
- [API Overview](https://www.md2wechat.com/docs/api)
- [OpenAPI JSON](https://www.md2wechat.com/openapi.v1.json)
- [llms.txt](https://www.md2wechat.com/llms.txt)
- [主题目录](https://www.md2wechat.com/themes)
- [Errors](https://www.md2wechat.com/docs/errors)
- [完整 CLI/Skill 仓库](https://github.com/geekjourneyx/md2wechat-skill)
- [完整 CLI 的 Skill SOP](https://github.com/geekjourneyx/md2wechat-skill/blob/main/skills/md2wechat/SKILL.md)
- [完整 CLI 使用文档](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/USAGE.md)
- [完整 CLI 配置文档](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/CONFIG.md)
- [完整 CLI Discovery 文档](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/DISCOVERY.md)
- [微信凭证与白名单](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/WECHAT-CREDENTIALS.md)
- [图片 Provider](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/IMAGE_PROVISIONERS.md)
- [宿主 Agent 图片计划](https://github.com/geekjourneyx/md2wechat-skill/blob/main/docs/AGENT_IMAGE_GEN.md)
- [轻量 CLI 仓库](https://github.com/geekjourneyx/md2wechat-lite)
- [完整仓库许可证](https://github.com/geekjourneyx/md2wechat-skill/blob/main/LICENSE)
- [轻量仓库许可证](https://github.com/geekjourneyx/md2wechat-lite/blob/main/LICENSE)
