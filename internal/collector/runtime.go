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
	"go.opentelemetry.io/collector/otelcol"
)

type RuntimeOptions struct{ Backend, ConfigPath, DataPath, ControlSocket, ControlToken string }

func Run(ctx context.Context, options RuntimeOptions) error {
	if options.ControlSocket == "" || options.ControlToken == "" {
		return errors.New("collector runtime requires its private control socket and token")
	}
	if options.Backend != "native" && options.Backend != "docker" {
		return fmt.Errorf("unknown collector backend %q", options.Backend)
	}
	cfg, err := config.ReadCollector(options.ConfigPath)
	if err != nil {
		return err
	}
	var col *otelcol.Collector
	if options.Backend == "native" {
		cfg, err = config.ReadNative(options.ConfigPath, options.DataPath)
		if err != nil {
			return err
		}
		col, err = newNativeCollector(cfg)
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	listener, err := net.Listen("unix", options.ControlSocket)
	if err != nil {
		return fmt.Errorf("opening runtime control socket: %w", err)
	}
	defer func() { os.Remove(options.ControlSocket); os.Remove(filepath.Dir(options.ControlSocket)) }()
	if err := os.Chmod(options.ControlSocket, 0o600); err != nil {
		listener.Close()
		return err
	}
	worker := ingestion.New(cfg.Ingestion)
	shutdownDone := make(chan struct{})
	var result runtimeShutdownResult
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+options.ControlToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		status := Status{Backend: options.Backend, PID: os.Getpid(), Ingestion: worker.Status()}
		if col != nil {
			status.Running = col.GetState() == otelcol.StateRunning
			status.Healthy = status.Running
		} else {
			running, err := inspectDockerRunning(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			status.Running = running
			status.Healthy = status.Running && checkHealth()
		}
		status.Healthy = status.Healthy && (!status.Ingestion.Enabled || status.Ingestion.Running && status.Ingestion.LastSuccess != nil && status.Ingestion.LastError == "")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+options.ControlToken {
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
		ingestDone <- worker.Run(ingestCtx, options.DataPath, func(err error) { fmt.Fprintln(os.Stderr, "lotel: automatic ingestion:", err) })
	}()
	collectDone := make(chan error, 1)
	if col != nil {
		go func() { collectDone <- col.Run(ctx) }()
	} else {
		go func() { collectDone <- monitorDocker(ctx) }()
	}
	select {
	case err = <-collectDone:
	case controlErr := <-controlErrors:
		if !errors.Is(controlErr, http.ErrServerClosed) {
			err = controlErr
		}
		cancel()
		if col != nil {
			col.Shutdown()
		}
		err = errors.Join(err, <-collectDone)
	case <-ctx.Done():
		if col != nil {
			col.Shutdown()
		}
		err = <-collectDone
	}
	if col == nil {
		err = errors.Join(err, stopDocker(context.Background()))
	}
	stopIngestion()
	<-ingestDone
	if cfg.Ingestion.Enabled {
		finalCtx, cancelFinal := context.WithTimeout(context.Background(), 5*time.Second)
		err = errors.Join(err, ingestFinal(finalCtx, options.DataPath))
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
func monitorDocker(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		running, err := inspectDockerRunning(ctx)
		if err == nil && !running {
			return errors.New("Collector container exited")
		}
		// An unavailable Docker API does not prove that collection has stopped.
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func finishUnmanagedDocker(ctx context.Context, state *State) error {
	if err := stopDocker(ctx); err != nil {
		return err
	}
	cfg, err := config.ReadCollector(state.ConfigPath)
	if err != nil {
		return err
	}
	if cfg.Ingestion.Enabled {
		return ingestFinal(ctx, state.DataPath)
	}
	return nil
}
