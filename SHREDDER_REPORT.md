# Shredder Report

## Existing Behavior Map
- Go module rooted at `src/reflector` with entrypoint `cmd/reflector/main.go`.
- Legacy implementation used raw `client-go` watch loops for Namespaces, Secrets, and ConfigMaps.
- Reflection behavior is driven by `reflector.v1.k8s.emberstack.com/*` annotations for allow/auto/source metadata.
- Namespace allow rules support regex name lists and Kubernetes label selector syntax with OR semantics.
- Excluded namespaces use glob syntax (`*`, `?`) from `ES_Reflector__Watcher__ExcludedNamespaces`.
- Secrets with types prefixed by `helm.sh` are ignored.
- Health endpoints were served on port 8080.
- Baseline verification before rewrite: `go build ./...` and `go test ./...` both passed in `src/reflector`.

## Public Contracts to Preserve
- Module path: `github.com/emberstack/kubernetes-reflector`.
- Environment variables: `ES_Reflector__Watcher__Timeout`, `ES_Reflector__Watcher__ExcludedNamespaces`, `ES_Ignite__KubernetesClient__SkipTlsVerify`, `ES_Serilog__MinimumLevel__Default`.
- Annotation keys and reflection semantics for Secrets and ConfigMaps.
- Auto-reflection cleanup, direct reflection sync behavior, and non-deletion of user-managed direct reflections when the source disappears.
- Health endpoints on port 8080.

## Rewrite Plan
- Replace watch-loop architecture with controller-runtime manager plus dedicated reconcilers for Secrets and ConfigMaps.
- Introduce a generic `ResourceReconciler[T]` with type-specific `ResourceOps`.
- Move glob and selector helpers into a single `mirror` package and migrate all tests to Ginkgo v2 + Gomega.
- Use controller-runtime fake clients and field indexes for reconciler unit tests.

## Verification Commands
- `cd /home/bdusad/projects/kubernetes-reflector/src/reflector && go mod tidy`
- `cd /home/bdusad/projects/kubernetes-reflector/src/reflector && go build ./...`
- `cd /home/bdusad/projects/kubernetes-reflector/src/reflector && go test ./...`

## Final Summary
- Rewrote the reflector implementation in `src/reflector` around controller-runtime.
- Added a generic `ResourceReconciler[T]`, shared field indexes, namespace-triggered source enqueueing, and type-specific Secret/ConfigMap adapters.
- Migrated all reflector tests to Ginkgo v2 + Gomega and added fake-client reconciler coverage for source, direct, auto, excluded-namespace, helm-secret, and ConfigMap flows.
- Files added: new controller-runtime entrypoint plus `internal/mirror/{glob,selector,properties,index,ops,reconciler,secret,configmap}` and Ginkgo suite/tests.
- Files removed: legacy `internal/glob`, `internal/selector`, `internal/mirror/mirror.go`, and `internal/mirror/errors.go` implementations.
- Dependencies added: `sigs.k8s.io/controller-runtime`, `github.com/onsi/ginkgo/v2`, `github.com/onsi/gomega`.
- Behavior preserved: reflection annotations, regex/selector matching, excluded namespace globs, helm secret exclusion, direct reflection retention when source vanishes, and health probes on `:8080`.
- Behavior intentionally modernized: controller-runtime manager/cache/reconciler architecture replaces manual watch-loop orchestration.
- Commands run successfully: `go mod tidy`, `go build ./...`, `go test ./...`.
- Known limitations: watcher timeout is now represented through manager sync period plus REST client timeout rather than hand-managed raw watch loops.
