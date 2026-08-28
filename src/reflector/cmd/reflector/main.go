package main

import (
	"os"
	"strings"
	"time"

	"github.com/emberstack/kubernetes-reflector/internal/config"
	"github.com/emberstack/kubernetes-reflector/internal/mirror"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	cfg := config.Load()

	level := zapcore.InfoLevel
	switch strings.ToLower(cfg.LogLevel) {
	case "debug", "verbose":
		level = zapcore.DebugLevel
	case "warning", "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	}

	opts := zap.Options{Level: level}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("main")

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Error(err, "failed to add scheme")
		os.Exit(1)
	}

	restCfg := ctrl.GetConfigOrDie()
	if cfg.SkipTLSVerify {
		restCfg.TLSClientConfig.Insecure = true
		restCfg.TLSClientConfig.CAFile = ""
		restCfg.TLSClientConfig.CAData = nil
	}

	syncPeriod := time.Duration(cfg.WatcherTimeout) * time.Second
	if syncPeriod > 0 {
		restCfg.Timeout = syncPeriod + 3*time.Second
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8080",
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
	})
	if err != nil {
		log.Error(err, "unable to create manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	if err := mirror.SetupIndexers(ctx, mgr); err != nil {
		log.Error(err, "failed to setup indexers")
		os.Exit(1)
	}

	secretReconciler := mirror.NewResourceReconciler(mgr.GetClient(), mirror.SecretOps{}, cfg, ctrl.Log.WithName("secrets"))
	if err := secretReconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to setup secret reconciler")
		os.Exit(1)
	}

	configMapReconciler := mirror.NewResourceReconciler(mgr.GetClient(), mirror.ConfigMapOps{}, cfg, ctrl.Log.WithName("configmaps"))
	if err := configMapReconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to setup configmap reconciler")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to add healthz check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "unable to add readyz check")
		os.Exit(1)
	}

	log.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "problem running manager")
		os.Exit(1)
	}
}
