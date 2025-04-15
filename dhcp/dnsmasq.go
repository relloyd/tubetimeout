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

	serviceStateStarted            = serviceState("active") // local is the primary dhcp server
	serviceStateRouterCanBeStopped = serviceState("router DHCP server can be stopped")
	serviceStateWaitingToStop      = serviceState("waiting to stop") // waiting for another DHCP server to be running
	serviceStateFailedCheckConfig  = serviceState("failed to start") // check config and retry
	serviceStateStopped            = serviceState("inactive")        //
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

	needsRestart bool
}

type Reservation struct {
	MacAddr models.MAC `yaml:"macAddr" json:"macAddr"` // use string type for MacAddr so it marshals to YAML nicely - we had issues implementing interfaces to make this happen on net.HardwareAddr.
	IpAddr  net.IP     `yaml:"ipAddr" json:"ipAddr"`
	Name    string     `yaml:"name" json:"name"`
}

func newDNSMasqConfig() *DNSMasqConfig {
	return &DNSMasqConfig{
		AddressReservations: make([]Reservation, 0),
		needsRestart:        true, // allow worker to (re)start dnsmasq for the first time
	}
}

type restarter interface {
	isDnsmasqServiceActive() (bool, error)
	isDNSMasqEnabledInConfig() bool
	isDHCPServerRunning(logger *zap.SugaredLogger, hwAddr net.HardwareAddr) (bool, bool, error) // updated to return two bools
	setStaticIP(logger *zap.SugaredLogger, ifaceName string, cfg *DNSMasqConfig, fnFinder cidrFinderFunc) error
	unsetStaticIP(logger *zap.SugaredLogger, ifaceName string) error
	startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error
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
	ticker := time.NewTicker(10 * time.Second)
	var err error
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
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
func (s *Server) maybeStartOrStopDnsmasq(logger *zap.SugaredLogger, svc restarter) (serviceState, error) {
	var err error
	numAttempts := 5
	dhcpRunningLocal := false
	dhcpRunningRouter := false
	wantStart := false
	wantEnabled := svc.isDNSMasqEnabledInConfig()

	for idx := 0; idx < numAttempts; idx++ {
		// Determine DHCP service status.
		dhcpRunningLocal, dhcpRunningRouter, err = svc.isDHCPServerRunning(logger, s.hwAddr)
		if err != nil { // if we should try again...
			logger.Warnf("Error while checking if DHCP server is running: %v", err)
			continue
		}

		// Maybe stop dnsmasq.
		if !wantEnabled { // if dnsmasq should be disabled by the user...
			if dhcpRunningLocal && dhcpRunningRouter { // if it's safe to disable the local dnsmasq...
				err := s.Stop()
				if err != nil {
					return dnsMasqConfig.ServiceState, err
				}
				return serviceStateStopped, nil
			} else if dhcpRunningLocal && !dhcpRunningRouter {
				return serviceStateWaitingToStop, nil
			} else { // else the local dnsmasq is not running...
				return serviceStateStopped, nil
			}
		}

		if wantEnabled { // if dnsmasq should be enabled by the user...
			if !dhcpRunningLocal || dnsMasqConfig.needsRestart { // if the local server isn't running, or it needs a restart...
				// We don't care if another server is running on the router.
				// Prefer to have two DHCP services running than none at all.
				wantStart = true
			}
		}

		// (Re)start the service.
		if wantStart {
			pattern := "The local DHCP server %v running, attempting to (%v)start dnsmasq"
			if dhcpRunningLocal {
				logger.Info(fmt.Sprintf(pattern, "is", ""))
			} else {
				logger.Info(fmt.Sprintf(pattern, "is not", "re"))
			}

			// Set a static IP for this gateway.
			if err = svc.setStaticIP(logger, s.ifaceName, dnsMasqConfig, findSmallestSingleCIDR); err != nil {
				logger.Warnf("Failed to set static IP on interface %s on attempt %d: %v", s.ifaceName, numAttempts+1, err)
				continue
			}

			// Start dnsmasq.
			if err = svc.startDnsmasq(logger, dnsMasqConfig); err != nil { // if dnsmasq failed to started...
				logger.Warnf("Failed to start dnsmasq on attempt %d: %v", numAttempts+1, err)
				if err := svc.unsetStaticIP(logger, s.ifaceName); err != nil { // reset to dynamic IP allocation in case we need another DHCP server to get an IP for web access.
					logger.Warnf("Failed to unset static IP on interface %s on attempt %d: %v", s.ifaceName, numAttempts+1, err)
				}
				continue // retry in case of failure
			}

			logger.Info("Successfully started dnsmasq")
			dnsMasqConfig.needsRestart = false // dnsMasqConfig.needsRestart set false to prevent restarts until config changes.

			if dhcpRunningRouter {
				return serviceStateRouterCanBeStopped, nil
			}
			return serviceStateStarted, nil
		}
	}

	// All attempts failed.
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
	dnsMasqConfig.needsRestart = true
	s.chanWorker <- struct{}{}
}
