package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/emberstack/kubernetes-reflector/internal/config"
	reflectorglob "github.com/emberstack/kubernetes-reflector/internal/glob"
	"github.com/emberstack/kubernetes-reflector/internal/mirror"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	cfg := config.Load()
	logger := buildLogger(cfg.LogLevel)
	defer logger.Sync() //nolint:errcheck

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	kubeConfig, err := buildKubeConfig(cfg.SkipTLSVerify)
	if err != nil {
		logger.Fatal("failed to build kube config", zap.Error(err))
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		logger.Fatal("failed to create k8s client", zap.Error(err))
	}

	excludedPatterns := reflectorglob.ParsePatterns(strings.ToLower(cfg.ExcludedNamespaces))
	listNamespaces := func(ctx context.Context) ([]*corev1.Namespace, error) {
		list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		result := make([]*corev1.Namespace, len(list.Items))
		for i := range list.Items {
			result[i] = &list.Items[i]
		}
		return result, nil
	}

	secretMirror := mirror.NewResourceMirror(mirror.NewSecretOperations(client), logger.Named("secret-mirror"), listNamespaces)
	configMapMirror := mirror.NewResourceMirror(mirror.NewConfigMapOperations(client), logger.Named("configmap-mirror"), listNamespaces)

	events := make(chan mirror.WatchEvent, 1024)
	closedEvents := make(chan mirror.WatcherClosedEvent, 16)
	watcherTimeout := int64(cfg.WatcherTimeout)

	go runWatcher(ctx, logger.Named("namespace-watcher"), "Namespace", watcherTimeout, excludedPatterns, func(ctx context.Context, timeout int64) (<-chan watch.Event, func(), error) {
		w, err := client.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{TimeoutSeconds: &timeout})
		if err != nil {
			return nil, nil, err
		}
		return w.ResultChan(), w.Stop, nil
	}, events, closedEvents)

	go runWatcher(ctx, logger.Named("secret-watcher"), "Secret", watcherTimeout, excludedPatterns, func(ctx context.Context, timeout int64) (<-chan watch.Event, func(), error) {
		w, err := client.CoreV1().Secrets("").Watch(ctx, metav1.ListOptions{TimeoutSeconds: &timeout})
		if err != nil {
			return nil, nil, err
		}
		return w.ResultChan(), w.Stop, nil
	}, events, closedEvents)

	go runWatcher(ctx, logger.Named("configmap-watcher"), "ConfigMap", watcherTimeout, excludedPatterns, func(ctx context.Context, timeout int64) (<-chan watch.Event, func(), error) {
		w, err := client.CoreV1().ConfigMaps("").Watch(ctx, metav1.ListOptions{TimeoutSeconds: &timeout})
		if err != nil {
			return nil, nil, err
		}
		return w.ResultChan(), w.Stop, nil
	}, events, closedEvents)

	go runHealthServer(ctx, logger.Named("health"), ":8080")

	mirrors := []*mirror.ResourceMirror{secretMirror, configMapMirror}
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down")
			return
		case e := <-closedEvents:
			for _, m := range mirrors {
				m.HandleClosed(e)
			}
		case e := <-events:
			for _, m := range mirrors {
				if err := m.HandleEvent(ctx, e); err != nil {
					logger.Error("handle event error", zap.Error(err))
				}
			}
		}
	}
}

func runWatcher(ctx context.Context, logger *zap.Logger, resourceType string, timeoutSecs int64, excludedPatterns []*regexp.Regexp,
	openWatch func(ctx context.Context, timeout int64) (<-chan watch.Event, func(), error),
	events chan<- mirror.WatchEvent, closed chan<- mirror.WatcherClosedEvent) {
	for {
		if ctx.Err() != nil {
			return
		}

		faulted := false
		start := time.Now()
		logger.Info("starting watch", zap.String("type", resourceType))

		ch, stop, err := openWatch(ctx, timeoutSecs+3)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("failed to open watch", zap.Error(err))
			faulted = true
			time.Sleep(5 * time.Second)
		} else {
			excludedCount := int64(0)
			for event := range ch {
				obj, ok := event.Object.(metav1.Object)
				if !ok {
					continue
				}

				ns := strings.ToLower(obj.GetNamespace())
				if reflectorglob.IsExcluded(ns, excludedPatterns) {
					excludedCount++
					continue
				}

				if secret, ok := event.Object.(*corev1.Secret); ok {
					if strings.HasPrefix(string(secret.Type), "helm.sh") {
						continue
					}
				}

				events <- mirror.WatchEvent{Type: mirror.EventType(event.Type), Object: event.Object}
			}

			stop()

			duration := time.Since(start)
			if excludedCount > 0 {
				logger.Info("watch session closed", zap.Duration("duration", duration), zap.Bool("faulted", faulted), zap.Int64("excluded", excludedCount))
			} else {
				logger.Info("watch session closed", zap.Duration("duration", duration), zap.Bool("faulted", faulted))
			}
		}

		closed <- mirror.WatcherClosedEvent{ResourceType: resourceType, Faulted: faulted}
		if ctx.Err() != nil {
			return
		}
		if faulted {
			time.Sleep(5 * time.Second)
		}
	}
}

func buildKubeConfig(skipTLSVerify bool) (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, err
		}
	}

	if skipTLSVerify {
		config.TLSClientConfig.Insecure = true
		config.TLSClientConfig.CAFile = ""
		config.TLSClientConfig.CAData = nil
	}

	return config, nil
}

func buildLogger(level string) *zap.Logger {
	var zapLevel zap.AtomicLevel
	switch strings.ToLower(level) {
	case "debug", "verbose":
		zapLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "warning", "warn":
		zapLevel = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapLevel = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zapLevel
	logger, _ := cfg.Build()
	return logger
}

func runHealthServer(ctx context.Context, logger *zap.Logger, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("health server error", zap.Error(err))
	}
}
