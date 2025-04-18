package dhcp

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"runtime"
	"strings"
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
	defaultGetConfig         = GetConfig                 // allow mocking
	defaultSetConfig         = SetConfig                 // allow mocking
	defaultDhcpService       = restarter(&dhcpService{}) // allow mocking
	routeCmd                 = defaultRouteCmd
	routeCmdArgs             = []string{"-rn"}
	configFileDNSMasqService = "/etc/dnsmasq.conf"
	configFileDHCPSettings   = "dhcp-config.yaml"
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
	isDNSMasqEnabledInConfig(cfg *DNSMasqConfig) bool
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
	cfg         *DNSMasqConfig
	ifaceName   string
	hwAddr      net.HardwareAddr
}

func NewServer(ctx context.Context, logger *zap.SugaredLogger) (*Server, error) {
	s := &Server{
		logger:      logger,
		chanWorker:  make(chan struct{}, 2),
		dhcpService: defaultDhcpService,
		// cfg:         newDNSMasqConfig(),
	}

	_, err := s.GetConfig() // GetConfig() sets the server config anyway.
	if err != nil {
		return nil, err
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
			s.cfg.ServiceState, err = s.maybeStartOrStopDnsmasq(s.logger, s.dhcpService)
			if err != nil {
				s.logger.Errorf("Worker: %v", err)
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
	state = s.cfg.ServiceState
	err = nil

	numAttempts := 1 // numAttempts set to 1 since we call this function repeatably from the worker anyway.
	dhcpRunningLocal := false
	dhcpRunningRouter := false
	wantEnabled := svc.isDNSMasqEnabledInConfig(s.cfg)

	defer func() {
		if state == serviceStateActive || state == serviceStateInactive { // if the service make it all the way up or down...
			s.cfg.needsAction = false
		}
	}()

	if !s.cfg.needsAction { // if there is nothing to do...
		return
	}

	for idx := 0; idx < numAttempts; idx++ {
		// Determine DHCP service status.
		dhcpRunningLocal, dhcpRunningRouter, err = svc.isDHCPServerRunning(logger, s.hwAddr)
		if err != nil { // if we should try again...
			logger.Errorf("Error checking if DHCP server is running: %v", err)
			continue
		}

		// Maybe stop dnsmasq.
		if !wantEnabled { // if dnsmasq is disabled by the user...
			if dhcpRunningLocal && dhcpRunningRouter { // if it's safe to disable the local dnsmasq...
				err = s.Stop()
				if err != nil {
					return
				}
				state = serviceStateInactive
				return
			} else if dhcpRunningLocal { // else if dhcpRunningRouter is false...
				// We are waiting for the router DHCP server to be stopped.
				state = serviceStateWaitingToStop
				return
			} else { // else the local dnsmasq is not running...
				state = serviceStateInactive
				return
			}
		} else {                   // else dnsmasq is enabled by the user...
			if !dhcpRunningLocal { // if the local server isn't running, or it needs a restart...
				// (Re)start the service.
				// needsAction is set to true when the config file is changed.
				// We don't care if another server is running on the router.
				// Prefer to have two DHCP services running than none at all and advise the user to stop the
				// router DHCP service via web interface.
				pattern := "The local DHCP server %v running, attempting to %vstart dnsmasq"
				if dhcpRunningLocal {
					logger.Info(fmt.Sprintf(pattern, "is", "re"))
				} else {
					logger.Info(fmt.Sprintf(pattern, "is not", ""))
				}

				// Start dnsmasq.
				if err = svc.startDnsmasq(logger, s.cfg, s.ifaceName, s.hwAddr); err != nil { // if dnsmasq failed to started...
					logger.Errorf("Failed to start dnsmasq on attempt %d: %v", numAttempts+1, err)
					continue // retry in case of failure
				}

				if dhcpRunningRouter {
					state = serviceStateActiveRouterCanBeStopped
					return
				}

				state = serviceStateActive
				return
			}
		}
	}

	// All attempts failed; try to force start dnsmasq anyway.
	isLocalActive, _ := svc.isDnsmasqServiceActive()
	if wantEnabled && !isLocalActive { // if dnsmasq config is enabled but it's still not running...
		logger.Errorf("Attempting to force start dnsmasq after %v failed attempts", numAttempts)
		if err = svc.startDnsmasq(logger, s.cfg, s.ifaceName, s.hwAddr); err == nil { // if dnsmasq started...
			state = serviceStateActive
			return
		} else {
			logger.Error("Failed to force start dnsmasq")
		}
	}

	state = serviceStateFailedCheckConfig
	err = fmt.Errorf("failed to start dnsmasq after %d attempt(s)", numAttempts)
	return
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

func (s *Server) GetConfig() (*DNSMasqConfig, error) {
	// Allow lazy mocking of the func that gets config so we don't have to mock
	// the whole inner workings of config.GetConfig in tests.
	return defaultGetConfig(s.logger, s.cfg)
}

func GetConfig(logger *zap.SugaredLogger, cfg *DNSMasqConfig) (*DNSMasqConfig, error) {
	if cfg != nil { // if dns config is already loaded...
		return cfg, nil
	}

	// Load from file.
	newCfg, err := config.GetConfig[*DNSMasqConfig](dhcpMutex, configFileDHCPSettings, newDNSMasqConfig)
	if err != nil {
		logger.Warnf("Failed to get dnsmasq config: %v", err)
		return nil, fmt.Errorf("failed to get dnsmasq config: %w", err)
	}

	// Assume defaults if empty.
	if newCfg.DefaultGateway == nil {
		newCfg.DefaultGateway, err = getDefaultGateway()
		if err != nil {
			logger.Warnf("Failed to get default gateway: %v", err)
			return nil, fmt.Errorf("failed to get default gateway: %w", err)
		}
	}

	ifaceName, err := getPrimaryInterfaceName()
	if err != nil {
		logger.Warnf("Failed to get primary interface (check your o/s is listed): %v", err)
		return nil, fmt.Errorf("failed to get primary interface (check your o/s is listed): %w", err)
	}

	if newCfg.LowerBound == nil || newCfg.UpperBound == nil || newCfg.ThisGateway == nil {
		lowerBound, upperBound, err := getSubnetBoundsForInterface(ifaceName)
		if err != nil {
			logger.Warnf("Failed to get subnet range for interface %s: %v", ifaceName, err)
			return nil, fmt.Errorf("failed to get subnet range for interface %s: %w", ifaceName, err)
		}
		newCfg.LowerBound, newCfg.UpperBound, newCfg.ThisGateway, err = adjustSubnetRange(lowerBound, upperBound, newCfg.DefaultGateway)
		if err != nil {
			logger.Warnf("Failed to adjust subnet range for interface %s: %v", ifaceName, err)
			return nil, fmt.Errorf("failed to adjust subnet range for interface %s: %w", ifaceName, err)
		}
	}

	if len(newCfg.DnsIPs) == 0 {
		newCfg.DnsIPs = fallbackDNSIPs
	}

	// Set the package global variable to the new value.
	cfg = newCfg

	return newCfg, nil
}

func (s *Server) SetConfig(logger *zap.SugaredLogger, newCfg *DNSMasqConfig) error {
	// Allow lazy mocking of the func that gets config so we don't have to mock
	// the whole inner workings of config.GetConfig in tests.
	err := defaultSetConfig(logger, s.cfg, newCfg)
	if err != nil {
		return err
	}
	s.restart()
	return nil
}

func SetConfig(_ *zap.SugaredLogger, oldCfg *DNSMasqConfig, newCfg *DNSMasqConfig) error {
	if newCfg == nil || oldCfg == nil {
		return fmt.Errorf("supplied new dnsmasq config is nil")
	}
	if oldCfg == nil {
		return fmt.Errorf("existing dnsmasq config is nil")
	}

	fnValidate := func(cfg *DNSMasqConfig) error {
		if cfg == nil {
			return fmt.Errorf("DNSMasqConfig is nil")
		}
		if cfg.DefaultGateway == nil || cfg.DefaultGateway.To4() == nil {
			return fmt.Errorf("invalid or missing DefaultGateway")
		}
		if cfg.ThisGateway == nil || cfg.ThisGateway.To4() == nil {
			return fmt.Errorf("invalid or missing ThisGateway")
		}
		if cfg.LowerBound == nil || cfg.LowerBound.To4() == nil {
			return fmt.Errorf("invalid or missing LowerBound")
		}
		if cfg.UpperBound == nil || cfg.UpperBound.To4() == nil {
			return fmt.Errorf("invalid or missing UpperBound")
		}
		if len(cfg.DnsIPs) == 0 {
			cfg.DnsIPs = fallbackDNSIPs
		}
		if bytes.Compare(cfg.LowerBound, cfg.UpperBound) >= 0 {
			return fmt.Errorf("LowerBound must be less than UpperBound")
		}
		for _, v := range cfg.AddressReservations { // for each address reservation...
			v.MacAddr = models.MAC(strings.ToUpper(strings.ReplaceAll(string(v.MacAddr), ":", "-"))) // Ensure upper case and hyphens.
		}
		cfg.needsAction = true // assume something has changed for now and that we want a restart
		return nil
	}

	fnUpdateInMem := func(cfg *DNSMasqConfig) {
		oldCfg = cfg
	}

	err := config.SetConfig[*DNSMasqConfig](dhcpMutex, configFileDHCPSettings, fnValidate, fnUpdateInMem, newCfg) // TODO: validate the incoming config but don't override any yet
	if err != nil {
		return fmt.Errorf("failed to set dnsmasq config: %w", err)
	}

	return nil
}

func (s *Server) restart() {
	s.cfg.needsAction = true
	s.chanWorker <- struct{}{}
}

func needsRestart(cfg *DNSMasqConfig) bool {
	return cfg.needsAction
}
