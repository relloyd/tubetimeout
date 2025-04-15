package dhcp

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

func init() {
	if runtime.GOOS == "linux" {
		cmd := "nmcli"
		err := config.CheckCmdAvailability(cmd)
		if err != nil {
			config.MustGetLogger().Fatalf("Error: %v. Please ensure the '%v' command is installed and available on your PATH.", cmd, err)
		}
	}
}

type systemctlAction string
type serviceState string

const (
	serviceRestart = systemctlAction("restart")
	serviceStop    = systemctlAction("stop")

	serviceStateActive                   = serviceState("active") // local is the primary dhcp server
	serviceStateActiveRouterCanBeStopped = serviceState("router DHCP server can be stopped")
	serviceStateWaitingToStop            = serviceState("waiting to stop") // waiting for another DHCP server to be running
	serviceStateFailedCheckConfig        = serviceState("failed to start") // check config and retry
	serviceStateInactive                 = serviceState("inactive")        //
)

var (
	defaultInterfaceName     = "eth0"
	defaultLeaseDuration     = "12h"
	defaultGetConfig         = GetConfig // allow mocking
	defaultSetConfig         = SetConfig // allow mocking
	routeCmd                 = defaultRouteCmd
	routeCmdArgs             = []string{"-rn"}
	configFileDNSMasqService = "/etc/dnsmasq.conf"
	configFileDHCPSettings   = "dhcp-config.yaml"
	dnsMasqConfig            *DNSMasqConfig                                             // expect NewServer() to set dnsMasqConfig up.
	defaultDhcpService       = &dhcpService{}                                           // allow mocking
	fallbackDNSIPs           = []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")} // default DNS IPs to CloudFlare and Google.
	dhcpMutex                = &sync.Mutex{}
)

type DNSMasqConfig struct {
	DefaultGateway      net.IP        `yaml:"defaultGateway" json:"defaultGateway"`
	ThisGateway         net.IP        `yaml:"thisGateway" json:"thisGateway"`
	LowerBound          net.IP        `yaml:"lowerBound" json:"lowerBound"`
	UpperBound          net.IP        `yaml:"upperBound" json:"upperBound"`
	DnsIPs              []net.IP      `yaml:"dnsIPs" json:"dnsIPs"`
	AddressReservations []Reservation `yaml:"addressReservations" json:"addressReservations"`
	ServiceEnabled      bool          `yaml:"serviceEnabled" json:"serviceEnabled"` // want state
	ServiceState        serviceState  `yaml:"serviceState" json:"serviceState"`     // current state // TODO: put the service into this state at boot time

	needsAction bool
}

type Reservation struct {
	MacAddr models.MAC `yaml:"macAddr" json:"macAddr"` // use string type for MacAddr so it marshals to YAML nicely - we had issues implementing interfaces to make this happen on net.HardwareAddr.
	IpAddr  net.IP     `yaml:"ipAddr" json:"ipAddr"`
	Name    string     `yaml:"name" json:"name"`
}

func newDNSMasqConfig() *DNSMasqConfig {
	return &DNSMasqConfig{
		AddressReservations: make([]Reservation, 0),
		needsAction:         true, // allow worker to (re)start dnsmasq for the first time
	}
}

type restarter interface {
	isDnsmasqServiceActive() (bool, error)
	isDNSMasqEnabledInConfig() bool
	isDHCPServerRunning(logger *zap.SugaredLogger, hwAddr net.HardwareAddr) (bool, bool, error) // updated to return two bools
	setStaticIP(logger *zap.SugaredLogger, ifaceName string, cfg *DNSMasqConfig, fnFinder cidrFinderFunc) error
	unsetStaticIP(logger *zap.SugaredLogger, ifaceName string) error
	startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig, ifaceName string, hwAddr net.HardwareAddr) error
	setDnsmasqServiceState(action systemctlAction) error
}

type Server struct {
	logger      *zap.SugaredLogger
	chanWorker  chan struct{}
	dhcpService restarter
	ifaceName   string
	hwAddr      net.HardwareAddr
}

func NewServer(ctx context.Context, logger *zap.SugaredLogger) (*Server, error) {
	var err error

	dnsMasqConfig, err = defaultGetConfig(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get dnsmasq config: %w", err)
	}

	s := &Server{
		logger:      logger,
		chanWorker:  make(chan struct{}, 2),
		dhcpService: defaultDhcpService,
	}

	s.ifaceName, err = getPrimaryInterfaceName()
	if err != nil {
		return nil, fmt.Errorf("failed to get primary interface: %w", err)
	}
	s.hwAddr, err = getIfaceHardwareAddress(s.ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get hardware address for interface %s: %w", s.ifaceName, err)
	}

	go s.startWorker(ctx)
	s.restart() // initial startup.

	return s, nil
}

func (s *Server) startWorker(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	var err error
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			// Generate synthetic events to trigger the refresh of dnsmasq service state:
			// If the service is on the way up:
			// - The user may have configured dnsmasq to be enabled in the config file but the router DHCP service
			//   may still be running so we advise the user via status.
			// If the service is on the way down:
			// - The user may have configured dnsmasq to be disabled in the config file but it may not be safe to
			//   disable dnsmasq yet. We advise the user to enable another DHCP service.
			s.chanWorker <- struct{}{}
		case <-s.chanWorker:
			dhcpMutex.Lock()
			dnsMasqConfig.ServiceState, err = s.maybeStartOrStopDnsmasq(s.logger, s.dhcpService)
			if err != nil {
				s.logger.Errorf("Worker failed to start dnsmasq: %v", err)
			}
			dhcpMutex.Unlock()
		}
	}
}

// maybeStartOrStopDnsmasq checks if it's okay to start dnsmasq based on config.
// If the service is config disabled then return false without an error.
// Return true if config wants dnsmasq started and the service could be started,
// i.e. there isn't already a DHCP server on the network.
// If there is a DHCP server on the network then return false and an error.
func (s *Server) maybeStartOrStopDnsmasq(logger *zap.SugaredLogger, svc restarter) (state serviceState, err error) {
	numAttempts := 5
	dhcpRunningLocal := false
	dhcpRunningRouter := false
	wantStart := false
	wantEnabled := svc.isDNSMasqEnabledInConfig()

	defer func() {
		if state == serviceStateActive || state == serviceStateInactive { // if the service make it all the way up or down...
			dnsMasqConfig.needsAction = false
		}
	}()

	if !dnsMasqConfig.needsAction { // if there is nothing to do...
		return dnsMasqConfig.ServiceState, nil
	}

	for idx := 0; idx < numAttempts; idx++ {
		// Determine DHCP service status.
		dhcpRunningLocal, dhcpRunningRouter, err = svc.isDHCPServerRunning(logger, s.hwAddr)
		if err != nil { // if we should try again...
			logger.Warnf("Error while checking if DHCP server is running: %v", err)
			continue
			// TODO: if we come back from a reboot and there is no DHCP server anywhere then we should try to start ours
		}

		// Maybe stop dnsmasq.
		if !wantEnabled { // if dnsmasq is disabled by the user...
			if dhcpRunningLocal && dhcpRunningRouter { // if it's safe to disable the local dnsmasq...
				err := s.Stop()
				if err != nil {
					return dnsMasqConfig.ServiceState, err
				}
				return serviceStateInactive, nil
			} else if dhcpRunningLocal  { // else if dhcpRunningRouter is false...
				// We are waiting for the router DHCP server to be stopped.
				return serviceStateWaitingToStop, nil
			} else { // else the local dnsmasq is not running...
				return serviceStateInactive, nil
			}
		} else {                                                // else dnsmasq is enabled by the user...
			if !dhcpRunningLocal || dnsMasqConfig.needsAction { // if the local server isn't running, or it needs a restart...
				// needsRestart is set to true when the config file is changed.
				// We don't care if another server is running on the router.
				// Prefer to have two DHCP services running than none at all and advise the user to stop the
				// router DHCP service via web interface.
				wantStart = true
			}
		}

		// (Re)start the service.
		if wantStart {
			pattern := "The local DHCP server %v running, attempting to %vstart dnsmasq"
			if dhcpRunningLocal {
				logger.Info(fmt.Sprintf(pattern, "is", "re"))
			} else {
				logger.Info(fmt.Sprintf(pattern, "is not", ""))
			}

			// Start dnsmasq.
			if err = svc.startDnsmasq(logger, dnsMasqConfig, s.ifaceName, s.hwAddr); err != nil { // if dnsmasq failed to started...
				logger.Warnf("Failed to start dnsmasq on attempt %d: %v", numAttempts+1, err)
				continue // retry in case of failure
			}

			if dhcpRunningRouter {
				return serviceStateActiveRouterCanBeStopped, nil
			}

			return serviceStateActive, nil
		}
	}

	// All attempts failed; try to start dnsmasq anyway.
	if wantEnabled { // if dnsmasq config is enabled...
		logger.Errorf("Attempting to force start dnsmasq after %v failed attempts", numAttempts)
		if err = svc.startDnsmasq(logger, dnsMasqConfig, s.ifaceName, s.hwAddr); err != nil { // if dnsmasq failed to started...
			logger.Error("Failed to force start dnsmasq")
		}
		return serviceStateActive, nil
	}

	return serviceStateFailedCheckConfig, fmt.Errorf("failed to start dnsmasq after %d attempts", numAttempts)
}

func (s *Server) Stop() error {
	// Reset to dynamic IP allocation in case we need another DHCP server to issue an IP to us.
	if err := s.dhcpService.unsetStaticIP(s.logger, s.ifaceName); err != nil {
		s.logger.Warnf("Failed to unset static IP on interface during dnsmasq stop %v: %v", s.ifaceName, err)
		return fmt.Errorf("failed to unset static IP  during dnsmasq stop on interface %v: %w", s.ifaceName, err)
	}
	if err := s.dhcpService.setDnsmasqServiceState(serviceStop); err != nil {
		s.logger.Errorf("Error while stopping dnsmasq service: %v", err)
		return fmt.Errorf("failed to stop dnsmasq: %w", err)
	}
	s.logger.Info("Stopped dnsmasq service")
	return nil
}

func (s *Server) GetConfig(logger *zap.SugaredLogger) (*DNSMasqConfig, error) {
	return defaultGetConfig(logger)
}

func (s *Server) SetConfig(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error {
	err := defaultSetConfig(logger, cfg)
	if err != nil {
		return err
	}
	s.restart()
	return nil
}

func (s *Server) restart() {
	dnsMasqConfig.needsAction = true
	s.chanWorker <- struct{}{}
}

func needsRestart(cfg *DNSMasqConfig) bool {
	return cfg.needsAction
}