# Shredder Report

## Existing Behavior Map

**Language/Framework**: C# / .NET 10 (ASP.NET Web Application)
**Package manager**: NuGet (via Directory.Packages.props)
**Build command**: `dotnet build`
**Test command**: `dotnet test`
**Run command**: `dotnet ES.Kubernetes.Reflector.dll` (inside Docker)
**Dockerfile**: `src/ES.Kubernetes.Reflector/Dockerfile`

### Entry Point
`src/ES.Kubernetes.Reflector/Program.cs` - ASP.NET host, registers watchers and mirrors as hosted services.

### Architecture
Three background services watch Kubernetes resources:
- `NamespaceWatcher` — watches `V1Namespace` cluster-wide
- `SecretWatcher` — watches `V1Secret` across all namespaces
- `ConfigMapWatcher` — watches `V1ConfigMap` across all namespaces

All watchers fan-out events to two mirrors:
- `SecretMirror` — manages Secret reflections
- `ConfigMapMirror` — manages ConfigMap reflections

The `ResourceMirror<T>` base class implements all reconciliation logic.

### Configuration (Environment Variables from Helm)
| Helm value | Env Var | Purpose | Default |
|---|---|---|---|
| `configuration.watcher.timeout` | `ES_Reflector__Watcher__Timeout` | Max watcher lifetime (s) | 3600 |
| `configuration.watcher.excludedNamespaces` | `ES_Reflector__Watcher__ExcludedNamespaces` | Comma-separated glob patterns | `""` |
| `configuration.kubernetes.skipTlsVerify` | `ES_Ignite__KubernetesClient__SkipTlsVerify` | Skip TLS verification | `false` |
| `configuration.logging.minimumLevel` | `ES_Serilog__MinimumLevel__Default` | Log level | `Information` |

### Health Endpoints
- `GET /health/live` — liveness probe
- `GET /health/ready` — readiness probe

## Public Contracts to Preserve

### Annotation API (MUST NOT CHANGE)
All annotations use prefix `reflector.v1.k8s.emberstack.com`:
- `/reflection-allowed` — bool; enables reflection on source
- `/reflection-allowed-namespaces` — comma-separated regex patterns for allowed target namespaces
- `/reflection-allowed-namespaces-selector` — Kubernetes label selector for allowed target namespaces
- `/reflection-auto-enabled` — bool; enables automatic mirror creation
- `/reflection-auto-namespaces` — comma-separated regex patterns for auto-mirror namespaces
- `/reflection-auto-namespaces-selector` — Kubernetes label selector for auto-mirror namespaces
- `/reflects` — `namespace/name` of source; marks a mirror
- `/auto-reflects` — bool; marks an auto-created mirror
- `/reflected-version` — `resourceVersion` of source at last reflection
- `/reflected-at` — RFC3339 timestamp of last reflection

### Copied Fields
- Secrets: `data`
- ConfigMaps: `data` + `binaryData`

### Env Vars (Helm chart compatibility — MUST NOT CHANGE)
Same set as listed above.

### Health Endpoint Paths (MUST NOT CHANGE)
`/health/live` and `/health/ready`

## Rewrite Plan

**Target language**: Go 1.24+
**Go module**: `github.com/emberstack/kubernetes-reflector`
**K8s client**: `k8s.io/client-go`
**Logging**: `go.uber.org/zap`
**Source location**: `src/reflector/` (new)
**Test location**: `tests/reflector/` (new)
**New Dockerfile**: `src/reflector/Dockerfile`

### New Directory Layout
```
src/reflector/
  cmd/reflector/main.go        ← entry point
  internal/
    annotations/annotations.go ← annotation constants
    config/config.go           ← env var config
    glob/glob.go               ← glob namespace exclusion
    glob/glob_test.go
    selector/selector.go       ← k8s label selector parsing
    selector/selector_test.go
    mirror/
      properties.go            ← MirroringProperties + annotation parsing
      properties_test.go
      mirror.go                ← ResourceMirror core reconciliation
      secret.go                ← Secret-specific ops
      configmap.go             ← ConfigMap-specific ops
  go.mod
  go.sum
  Dockerfile
tests/reflector/               ← (tests live inside src/reflector per Go convention)
```

## Verification Commands

```bash
# Build
cd src/reflector && go build ./...

# Test
cd src/reflector && go test ./...

# Lint (if golangci-lint is available)
cd src/reflector && golangci-lint run

# Run locally (needs KUBECONFIG)
cd src/reflector && go run ./cmd/reflector

# Docker build
docker build -f src/reflector/Dockerfile -t kubernetes-reflector:dev .
```

## Final Summary

### What was rewritten
The entire application was rewritten from C# .NET 10 to Go 1.24.

### New architecture
```
src/reflector/
  cmd/reflector/main.go           ← entry point, watchers, health server
  internal/annotations/           ← annotation name constants
  internal/config/                ← env-var based config (same var names as C#)
  internal/glob/                  ← excluded-namespace glob matching
  internal/selector/              ← Kubernetes label selector parsing/matching
  internal/mirror/
    properties.go                 ← MirroringProperties, annotation parsing
    mirror.go                     ← ResourceMirror reconciliation engine
    secret.go                     ← Secret-specific K8s operations
    configmap.go                  ← ConfigMap-specific K8s operations
  go.mod / go.sum
  Dockerfile
```

### Files added
- `src/reflector/**` (all Go source)
- `SHREDDER_REPORT.md`

### Files changed
- `.github/workflows/pipeline.yaml` — replaced .NET build/test steps with Go; updated Dockerfile path

### Files NOT deleted (preserved on branch)
- `src/ES.Kubernetes.Reflector/` — C# source (legacy; delete when satisfied)
- `tests/ES.Kubernetes.Reflector.Tests/` — C# tests (legacy; delete when satisfied)
- `.sln`, `*.props`, `NuGet.config`, `Directory.*.props` — .NET project files

### Dependencies added
| Package | Purpose |
|---|---|
| `k8s.io/client-go v0.31.3` | Kubernetes client |
| `k8s.io/api v0.31.3` | Kubernetes API types |
| `k8s.io/apimachinery v0.31.3` | Kubernetes meta types |
| `go.uber.org/zap v1.27.0` | Structured logging |

### Dependencies removed
All NuGet packages (ES.FX.Ignite, Serilog, OpenTelemetry, k8s C# client, etc.)

### Behavior preserved
- All annotation names and semantics (public API)
- All reflection logic (direct, auto, label-selector matching)
- Namespace glob exclusion (ExcludedNamespaces)
- Health endpoints `/health/live` and `/health/ready` on port 8080
- All env var names read from Helm (Helm chart works unchanged)
- Skipping helm.sh/* secrets
- Watcher timeout (default 3600s)
- Namespace label caching to avoid redundant reconciliation
- JSON Patch for minimal updates to existing reflections

### Behavior intentionally changed
- Logging: Serilog replaced with go.uber.org/zap (JSON output format maintained)
- OpenTelemetry/Seq exporter removed (no direct Go equivalent for ES.FX.Ignite.OpenTelemetry.Exporter.Seq; add separately if needed)
- In-process health check framework replaced with a simple `net/http` handler

### Tests added
- `internal/glob/glob_test.go` — 15 tests (mirrors C# GlobMatcherTests)
- `internal/selector/selector_test.go` — 11 tests (mirrors C# LabelSelectorMatchTests)
- `internal/mirror/properties_test.go` — 6 tests (mirrors C# NamespaceLabelsEqualTests)

### Tests NOT migrated
- Integration tests (`tests/ES.Kubernetes.Reflector.Tests/Integration/`) — require K3s Testcontainers, need rewrite in Go

### Commands run
```
go mod tidy  ✅
go build ./...  ✅ (26 Go files compiled)
go test ./...   ✅ (32 tests passed, 0 failed)
```

### How to complete the migration (when ready)
1. Delete legacy C# files:
   ```bash
   rm -rf src/ES.Kubernetes.Reflector tests/ES.Kubernetes.Reflector.Tests
   rm -f ES.Kubernetes.Reflector.sln ES.Kubernetes.Reflector.sln.DotSettings
   rm -f Directory.Build.props Directory.Packages.props NuGet.config Shared.DotSettings
   ```
2. Write Go integration tests in `src/reflector/` using `kind` or `testcontainers-go/k3s`
3. Optionally add `golangci-lint` to CI

### Known limitations
- Integration tests not yet migrated to Go
- OpenTelemetry/tracing not yet wired up (was Seq-specific in C#)
- No Seq log exporter (log to stdout in JSON; ship with a log agent)


---

## Notes / Uncertainties

- The C# code uses `Regex` patterns in `AllowedNamespaces` / `AutoNamespaces` (not glob). These are passed through as-is from annotations. The Go implementation preserves this: comma-separated regex patterns matched with full-string anchoring.
- The glob matching in `ExcludedNamespaces` uses `*` / `?` wildcards (not regex). Go implementation matches this exactly.
- Helm chart env var names use mixed-case with `__` separators — Go reads them as-is, so the Helm chart works unchanged.
- The C# code sends events from ALL watchers to ALL mirrors. Mirrors type-assert the event payload to act on relevant types. Go implementation follows the same pattern.
- `WatcherClosed` events reset per-resource caches so watchers can replay cleanly on reconnect, while keeping `_namespaceCache` alive across reconnects for label-selector lookups.
