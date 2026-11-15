package collector

import (
	"github.com/mattsp1290/lotel/internal/config"
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

func newNativeCollector(cfg config.CollectorConfig) (*otelcol.Collector, error) {
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
