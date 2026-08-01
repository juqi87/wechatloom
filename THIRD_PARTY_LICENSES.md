# Third-party licenses

Runtime dependencies:

| Module | Version | License |
|---|---:|---|
| `github.com/yuin/goldmark` | `v1.8.2` | MIT |
| `go.yaml.in/yaml/v3` | `v3.0.4` | MIT and Apache-2.0 |
| `golang.org/x/image` | `v0.30.0` | BSD-3-Clause |
| `golang.org/x/text` | `v0.28.0` | BSD-3-Clause |

The dependency versions are locked in `go.sum`. CI runs a license-policy check for every package in the built command.
