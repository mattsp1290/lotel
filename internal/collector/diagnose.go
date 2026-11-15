package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func decodeDiagnosis(r io.Reader, result any) error {
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("diagnostic input exceeds limit")
	}
	return json.Unmarshal(data, result)
}

type RuntimeDiagnosis struct {
	StateFile DiagnosisState     `json:"state_file"`
	Runtime   DiagnosisRuntime   `json:"runtime"`
	Ingestion DiagnosisIngestion `json:"ingestion"`
}

type DiagnosisState struct {
	Observation string `json:"observation"`
	Reason      string `json:"reason,omitempty"`
}

type DiagnosisRuntime struct {
	Observation     string `json:"observation"`
	Reason          string `json:"reason,omitempty"`
	Running         *bool  `json:"running"`
	CombinedHealthy *bool  `json:"combined_healthy"`
}

type DiagnosisIngestion struct {
	Observation string     `json:"observation"`
	Enabled     *bool      `json:"enabled"`
	Running     *bool      `json:"running"`
	Interval    *string    `json:"interval"`
	LastSuccess *time.Time `json:"last_success"`
	HasError    *bool      `json:"has_error"`
}

type diagnosticStatus struct {
	Backend   string `json:"backend"`
	Running   *bool  `json:"running"`
	Healthy   *bool  `json:"healthy"`
	Ingestion *struct {
		Enabled     *bool           `json:"enabled"`
		Running     *bool           `json:"running"`
		Interval    *string         `json:"interval"`
		LastSuccess json.RawMessage `json:"last_success"`
		LastError   json.RawMessage `json:"last_error"`
	} `json:"ingestion"`
}

func DiagnoseRuntime(ctx context.Context) RuntimeDiagnosis {
	report := RuntimeDiagnosis{
		StateFile: DiagnosisState{Observation: "unavailable", Reason: "state_unavailable"},
		Runtime:   DiagnosisRuntime{Observation: "unknown", Reason: "state_unavailable"},
		Ingestion: DiagnosisIngestion{Observation: "unknown"},
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return report
	}
	path := filepath.Join(home, stateDir, stateFile)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		report.StateFile = DiagnosisState{Observation: "missing"}
		report.Runtime.Reason = "state_missing"
		return report
	}
	if err != nil {
		return report
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return report
	}
	file, err := os.Open(path)
	if err != nil {
		return report
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return report
	}
	var state State
	if err := decodeDiagnosis(file, &state); err != nil {
		report.StateFile.Reason = "state_invalid"
		report.Runtime.Reason = "state_invalid"
		return report
	}
	report.StateFile = DiagnosisState{Observation: "present"}
	if (state.Backend != "native" && state.Backend != "docker") || state.ControlSocket == "" || state.ControlToken == "" {
		report.Runtime.Reason = "state_invalid"
		return report
	}
	response, err := runtimeControlRequest(ctx, &state, http.MethodGet, "/status", 2*time.Second)
	if err != nil {
		report.Runtime = DiagnosisRuntime{Observation: "unavailable", Reason: "runtime_unavailable"}
		return report
	}
	defer response.Body.Close()
	var status diagnosticStatus
	if err := decodeDiagnosis(response.Body, &status); err != nil || status.Backend != state.Backend || status.Running == nil || status.Healthy == nil || status.Ingestion == nil || status.Ingestion.Enabled == nil || status.Ingestion.Running == nil || status.Ingestion.Interval == nil {
		report.Runtime = DiagnosisRuntime{Observation: "unavailable", Reason: "invalid_response"}
		return report
	}
	interval, err := time.ParseDuration(*status.Ingestion.Interval)
	if err != nil || interval <= 0 {
		report.Runtime = DiagnosisRuntime{Observation: "unavailable", Reason: "invalid_response"}
		return report
	}
	var lastError string
	if len(status.Ingestion.LastError) != 0 {
		if bytes.Equal(bytes.TrimSpace(status.Ingestion.LastError), []byte("null")) || json.Unmarshal(status.Ingestion.LastError, &lastError) != nil {
			report.Runtime = DiagnosisRuntime{Observation: "unavailable", Reason: "invalid_response"}
			return report
		}
	}
	var lastSuccess *time.Time
	if len(status.Ingestion.LastSuccess) != 0 {
		var parsed time.Time
		if bytes.Equal(bytes.TrimSpace(status.Ingestion.LastSuccess), []byte("null")) || json.Unmarshal(status.Ingestion.LastSuccess, &parsed) != nil {
			report.Runtime = DiagnosisRuntime{Observation: "unavailable", Reason: "invalid_response"}
			return report
		}
		lastSuccess = &parsed
	}
	if (*status.Healthy && !*status.Running) ||
		(!*status.Ingestion.Enabled && (*status.Ingestion.Running || lastSuccess != nil || lastError != "")) ||
		(*status.Healthy && *status.Ingestion.Enabled && (!*status.Ingestion.Running || lastSuccess == nil || lastError != "")) {
		report.Runtime = DiagnosisRuntime{Observation: "unavailable", Reason: "invalid_response"}
		return report
	}
	intervalText := interval.String()
	hasError := lastError != ""
	report.Runtime = DiagnosisRuntime{Observation: "available", Running: status.Running, CombinedHealthy: status.Healthy}
	report.Ingestion = DiagnosisIngestion{Observation: "available", Enabled: status.Ingestion.Enabled, Running: status.Ingestion.Running, Interval: &intervalText, LastSuccess: lastSuccess, HasError: &hasError}
	return report
}
