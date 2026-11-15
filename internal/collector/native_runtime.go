package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mattsp1290/lotel/internal/config"
	"github.com/mattsp1290/lotel/internal/ingestion"
	"github.com/open-telemetry/opentelemetry-collector-contrib/connector/spanmetricsconnector"
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fileexporter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/healthcheckextension"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/genainormalizerprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/envprovider"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/featuregate"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
)

func nativeFactories() (otelcol.Factories, error) {
	f := otelcol.Factories{Telemetry: otelconftelemetry.NewFactory()}
	var err error
	f.Receivers, err = otelcol.MakeFactoryMap[receiver.Factory](otlpreceiver.NewFactory())
	if err != nil {
		return f, err
	}
	f.Processors, err = otelcol.MakeFactoryMap[processor.Factory](batchprocessor.NewFactory(), genainormalizerprocessor.NewFactory(), transformprocessor.NewFactory())
	if err != nil {
		return f, err
	}
	f.Exporters, err = otelcol.MakeFactoryMap[exporter.Factory](fileexporter.NewFactory())
	if err != nil {
		return f, err
	}
	f.Extensions, err = otelcol.MakeFactoryMap[extension.Factory](healthcheckextension.NewFactory())
	if err != nil {
		return f, err
	}
	f.Connectors, err = otelcol.MakeFactoryMap[connector.Factory](spanmetricsconnector.NewFactory())
	return f, err
}

func newNativeCollector(cfg config.NativeConfig) (*otelcol.Collector, error) {
	if err := featuregate.GlobalRegistry().Set("service.profilesSupport", true); err != nil {
		return nil, err
	}
	return otelcol.NewCollector(otelcol.CollectorSettings{
		Factories:               nativeFactories,
		BuildInfo:               component.BuildInfo{Command: "lotel", Description: "lotel native OpenTelemetry Collector", Version: "0.165.0"},
		DisableGracefulShutdown: true,
		ConfigProviderSettings: otelcol.ConfigProviderSettings{ResolverSettings: confmap.ResolverSettings{
			URIs:              []string{"yaml:" + string(cfg.YAML)},
			ProviderFactories: []confmap.ProviderFactory{yamlprovider.NewFactory(), envprovider.NewFactory(), fileprovider.NewFactory()},
			DefaultScheme:     "env",
		}},
	})
}

func RunNative(ctx context.Context, configPath, dataPath, socket, token string) error {
	if socket == "" || token == "" {
		return errors.New("native collector requires its private control socket and token")
	}
	cfg, err := config.ReadNative(configPath, dataPath)
	if err != nil {
		return err
	}
	col, err := newNativeCollector(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("opening native control socket: %w", err)
	}
	defer func() { os.Remove(socket); os.Remove(filepath.Dir(socket)) }()
	if err := os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		return err
	}
	shutdownDone := make(chan struct{})
	var result nativeShutdownResult
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Status{Running: col.GetState() != otelcol.StateClosed, Healthy: col.GetState() == otelcol.StateRunning, Backend: "native", PID: os.Getpid()})
	})
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		cancel()
		select {
		case <-shutdownDone:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		case <-r.Context().Done():
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	controlErrors := make(chan error, 1)
	go func() { controlErrors <- server.Serve(listener) }()
	defer server.Close()
	ingestCtx, stopIngestion := context.WithCancel(context.Background())
	ingestDone := make(chan error, 1)
	go func() {
		ingestDone <- ingestion.Run(ingestCtx, dataPath, cfg.Ingestion, func(err error) { fmt.Fprintln(os.Stderr, "lotel: automatic ingestion:", err) })
	}()
	collectDone := make(chan error, 1)
	go func() { collectDone <- col.Run(ctx) }()
	select {
	case err = <-collectDone:
	case controlErr := <-controlErrors:
		if !errors.Is(controlErr, http.ErrServerClosed) {
			err = controlErr
		}
		cancel()
		col.Shutdown()
		<-collectDone
	case <-ctx.Done():
		col.Shutdown()
		err = <-collectDone
	}
	stopIngestion()
	<-ingestDone
	if cfg.Ingestion.Enabled {
		finalCtx, cancelFinal := context.WithTimeout(context.Background(), 5*time.Second)
		err = errors.Join(err, ingestFinal(finalCtx, dataPath))
		cancelFinal()
	}
	if err != nil {
		result.Error = err.Error()
	}
	close(shutdownDone)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	return errors.Join(err, server.Shutdown(shutdownCtx))
}
