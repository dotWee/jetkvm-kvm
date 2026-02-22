package kvm

import (
	"github.com/jetkvm/kvm/internal/rdp"
)

var rdpServer *rdp.Server

// rdpLogAdapter adapts the zerolog-based logger to the rdp.Logger interface.
type rdpLogAdapter struct{}

func (rdpLogAdapter) Debug(msg string, args ...any) {
	rdpLogger.Debug().Msgf(msg, args...)
}

func (rdpLogAdapter) Info(msg string, args ...any) {
	rdpLogger.Info().Msgf(msg, args...)
}

func (rdpLogAdapter) Warn(msg string, args ...any) {
	rdpLogger.Warn().Msgf(msg, args...)
}

func (rdpLogAdapter) Error(msg string, args ...any) {
	rdpLogger.Error().Msgf(msg, args...)
}

// initRDPServer initializes and starts the RDP server if enabled in config.
func initRDPServer() {
	if config.RDPConfig == nil {
		return
	}

	cfg := *config.RDPConfig
	if !cfg.Enabled {
		rdpLogger.Info().Msg("RDP server is disabled")
		return
	}

	rdpServer = rdp.NewServer(cfg, nil, nil, rdpLogAdapter{})
	if err := rdpServer.Start(); err != nil {
		rdpLogger.Error().Err(err).Msg("failed to start RDP server")
		rdpServer = nil
		return
	}

	rdpLogger.Info().Int("port", cfg.Port).Msg("RDP server started")
}

// stopRDPServer stops the RDP server if running.
func stopRDPServer() {
	if rdpServer != nil {
		if err := rdpServer.Stop(); err != nil {
			rdpLogger.Error().Err(err).Msg("failed to stop RDP server")
		}
		rdpServer = nil
	}
}

// RDP RPC types

type rdpConfigResponse struct {
	Enabled     bool   `json:"enabled"`
	Port        int    `json:"port"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	TileSize    int    `json:"tile_size"`
	MaxSessions int    `json:"max_sessions"`
	Running     bool   `json:"running"`
	Sessions    int    `json:"sessions"`
}

func rpcGetRDPConfig() (rdpConfigResponse, error) {
	resp := rdpConfigResponse{}

	if config.RDPConfig != nil {
		resp.Enabled = config.RDPConfig.Enabled
		resp.Port = config.RDPConfig.Port
		resp.Width = int(config.RDPConfig.Width)
		resp.Height = int(config.RDPConfig.Height)
		resp.TileSize = config.RDPConfig.TileSize
		resp.MaxSessions = config.RDPConfig.MaxSessions
	}

	if rdpServer != nil {
		resp.Running = rdpServer.Addr() != ""
		resp.Sessions = rdpServer.ActiveSessions()
	}

	return resp, nil
}

type rdpSetConfigParams struct {
	Enabled     *bool `json:"enabled"`
	Port        *int  `json:"port"`
	MaxSessions *int  `json:"max_sessions"`
}

func rpcSetRDPConfig(params rdpSetConfigParams) error {
	if config.RDPConfig == nil {
		defaultCfg := rdp.DefaultConfig()
		config.RDPConfig = &defaultCfg
	}

	if params.Enabled != nil {
		config.RDPConfig.Enabled = *params.Enabled
	}
	if params.Port != nil {
		config.RDPConfig.Port = *params.Port
	}
	if params.MaxSessions != nil {
		config.RDPConfig.MaxSessions = *params.MaxSessions
	}

	config.RDPConfig.Validate()

	if err := SaveConfig(); err != nil {
		return err
	}

	// Restart RDP server with new config
	stopRDPServer()
	if config.RDPConfig.Enabled {
		initRDPServer()
	}

	return nil
}
