# Contributing to WeChatLoom

Contributions should preserve the documented module boundaries, source-read-only default, deterministic HTML contract, and explicit remote-write gate.

Before opening a pull request:

```bash
go test -race ./...
go vet ./...
go build ./cmd/wechatloom
```

For intentional visual changes, follow [docs/visual-regression.md](docs/visual-regression.md). Pixel baselines must be reviewed at 320, 375, and 430 px and must never be updated automatically by a test.

Add tests at an agreed public seam and demonstrate a failing test before production code. Do not include credentials, access tokens, captured private requests, copyrighted themes, or copied visual assets.

All commits must include a Developer Certificate of Origin sign-off:

```bash
git commit --signoff
```

By signing off, you certify the contribution under the repository license and the [Developer Certificate of Origin 1.1](https://developercertificate.org/).
