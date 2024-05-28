package main

import (
	"context"
	"fmt"

	"github.com/Layr-Labs/eigenda-proxy/eigenda"
	"github.com/Layr-Labs/eigenda-proxy/metrics"
	"github.com/Layr-Labs/eigenda-proxy/store"
	"github.com/Layr-Labs/eigenda-proxy/verify"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"

	proxy "github.com/Layr-Labs/eigenda-proxy"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	"github.com/ethereum-optimism/optimism/op-service/opio"
)

func LoadStore(cfg CLIConfig, ctx context.Context, log log.Logger) (proxy.Store, error) {
	if cfg.MemStoreCfg.Enabled {
		log.Info("Using memstore backend")
		return store.NewMemStore(ctx, &cfg.MemStoreCfg, log)
	}

	log.Info("Using eigenda backend")
	daCfg := cfg.EigenDAConfig

	v, err := verify.NewVerifier(daCfg.KzgConfig())
	if err != nil {
		return nil, err
	}

	return store.NewEigenDAStore(
		ctx,
		eigenda.NewEigenDAClient(
			log,
			daCfg,
		),
		v,
	)
}

func StartProxySvr(cliCtx *cli.Context) error {
	if err := CheckRequired(cliCtx); err != nil {
		return err
	}
	cfg := ReadCLIConfig(cliCtx)
	if err := cfg.Check(); err != nil {
		return err
	}
	ctx, ctxCancel := context.WithCancel(cliCtx.Context)
	defer ctxCancel()

	m := metrics.NewMetrics("default")

	log := oplog.NewLogger(oplog.AppOut(cliCtx), oplog.ReadCLIConfig(cliCtx)).New("role", "eigenda_proxy")
	oplog.SetGlobalLogHandler(log.Handler())

	log.Info("Initializing EigenDA proxy server...")

	da, err := LoadStore(cfg, ctx, log)
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}
	server := proxy.NewServer(cliCtx.String(ListenAddrFlagName), cliCtx.Int(PortFlagName), da, log, m)

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start the DA server")
	} else {
		log.Info("Started DA Server")
	}

	defer func() {
		if err := server.Stop(); err != nil {
			log.Error("failed to stop DA server", "err", err)
		}

		log.Info("successfully shutdown API server")
	}()

	if cfg.MetricsCfg.Enabled {
		log.Debug("starting metrics server", "addr", cfg.MetricsCfg.ListenAddr, "port", cfg.MetricsCfg.ListenPort)
		svr, err := m.StartServer(cfg.MetricsCfg.ListenAddr, cfg.MetricsCfg.ListenPort)
		if err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
		defer func() {
			if err := svr.Stop(context.Background()); err != nil {
				log.Error("failed to stop metrics server", "err", err)
			}
		}()
		log.Info("started metrics server", "addr", svr.Addr())
		m.RecordUp()
	}

	opio.BlockOnInterrupts()

	return nil
}