---
name: wechatloom
description: Orchestrate the local WeChatLoom CLI to turn Markdown into themed, deterministic WeChat Official Account HTML with component layout, safe remote and local images, formulas, Mermaid diagrams, interactive mobile preview, and PNG snapshots. Use when the user asks to convert or format a .md file for 微信公众号/WeChat Official Accounts, choose a WeChat theme, arrange an article with reusable components, inspect publishing readiness, preview mobile layouts, or prepare for a future WeChat draft; report that the current v0.3 development CLI has no WeChat remote-write command.
---

# WeChatLoom

Use Codex for editorial choices and the CLI for discovery, validation, deterministic rendering, hashing, assets, previews, and artifact writes.

## Resolve the CLI

Prefer an installed `wechatloom` binary. Inside the source repository, use:

```bash
go run ./cmd/wechatloom
```

Run `wechatloom capabilities --json` first. Treat runtime discovery as authoritative; do not hardcode theme or component counts.

Run `wechatloom skill status codex --json` when diagnosing Skill/CLI compatibility. If it reports `outdated`, `modified`, or `unmanaged`, report the state; never run `skill install` or `skill update` unless the user explicitly approves that local Codex configuration write.

## Plan the layout

1. Read the Markdown without changing it.
2. Run `wechatloom theme list --json`; recommend one theme based on article type and explain the choice briefly.
3. Run `wechatloom component list --json`. Use `component show <name> --json` before authoring a directive.
4. Prefer 3–5 purposeful components. Do not add the full component gallery to a normal article.
5. If the user asks for automatic arrangement, write a separate derived Markdown file unless they explicitly approve editing the source. Preserve facts and wording unless rewriting was also requested.

Use complete `:::wx-<name>` blocks and follow the discovered schema. Display formulas in `$$` fences and safe flow diagrams in fenced `mermaid` blocks. The v0.2 Mermaid subset accepts `flowchart`/`graph`, `LR`/`RL`/`TD`/`TB`, and `A --> B` edges; it rejects links, click handlers, styles, URLs, and executable markup.

## Build workflow

1. Locate the source Markdown and project root.
2. Keep the source file read-only unless the user explicitly requests a source edit.
3. Run `wechatloom init <project-root> --json` when `.wechatloom/project.yaml` is absent.
4. Run `wechatloom inspect <article.md> --json`.
5. Stop and report stable diagnostic codes when inspection is not ready. Do not silently discard invalid components.
6. Run `wechatloom build <article.md> --root <project-root> --theme <name> --json`. Omit `--theme` to use frontmatter and then `.wechatloom/project.yaml`.
7. Return `article_html_path`, `preview_html_path`, `manifest_path`, source hash, content hash, and warnings.
8. Offer `wechatloom preview <build_path>` for the read-only loopback preview. This command stays active until interrupted and opens the system browser unless `--no-open` is used.
9. Use `wechatloom snapshot <build_path> --json` when the user requests PNG review at 320, 375, and 430 px. A missing local Chrome/Chromium/Edge blocks snapshots only.

Parse stdout only as the JSON envelope. Treat stderr as diagnostics, never as protocol data.

## Safety gates

- Never claim that an article was published or saved to WeChat unless the installed CLI reports a confirmed remote result.
- The current `0.3.x` development implementation can materialize safe public remote Markdown images into local build artifacts, but still has no WeChat remote-write command.
- Never infer remote consent from consent to inspect, build, render, or preview.
- When draft support becomes available, require an explicit confirmation immediately before the command that writes remotely.
- Never store app secrets, access tokens, article bodies, or personal contact data in project configuration or protocol logs.
- Do not introduce network access unless the source actually contains a public HTTP/HTTPS image. Remote image handling must remain behind the CLI's SSRF, MIME, size, pixel, redirect, and timeout checks.

## Configuration

Read project defaults from `.wechatloom/project.yaml`. Keep shareable, non-secret settings there. Use the manifest as the build fact source and do not reconstruct build state from filenames.

Use `theme export <name>|--all --output <dir>`, `theme validate <theme.json>`, and `theme install <theme.json> --root <project>` for data-only theme packages. Never install a conflicting version with `--force` without telling the user. Use `component export --all --output <dir>` when the user needs portable schemas and valid/invalid examples.

## Content changes

Build without rewriting the article body by default. If the user requests rewriting, show or summarize the source diff before building. Put generated layout or derived content in build artifacts unless the user separately approves updating the source.
