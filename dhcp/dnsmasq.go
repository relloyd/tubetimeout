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
	defaultGetConfig         = GetConfig                 // allow mocking // TODO: stop using default funcs, but you'll need to update tests to be smarter
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

	needsAction  bool // needsAction allows worker to continually try to up the dnsmasq service until the router DHCP server is stopped.
	needsRestart bool // needsRestart allows dnsmasq to be restart once, until set false
}

type Reservation struct {
	MacAddr models.MAC `yaml:"macAddr" json:"macAddr"` // use string type for MacAddr so it marshals to YAML nicely - we had issues implementing interfaces to make this happen on net.HardwareAddr.
	IpAddr  net.IP     `yaml:"ipAddr" json:"ipAddr"`
	Name    string     `yaml:"name" json:"name"`
}

func newDNSMasqConfig() *DNSMasqConfig {
	return &DNSMasqConfig{
		AddressReservations: make([]Reservation, 0),
		needsAction:         true,
		needsRestart:        true,
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
	logger                         *zap.SugaredLogger
	chanWorker                     chan struct{}
	dhcpService                    restarter
	cfg                            *DNSMasqConfig
	ifaceName                      string
	hwAddr                         net.HardwareAddr
	dnsMasqServiceDisabledForDebug bool
	ledWarning                     LEDController
}

type LEDController interface {
	EnableWarning()
	DisableWarning()
}

func NewServer(ctx context.Context, logger *zap.SugaredLogger, dnsMasqServiceDisabledForDebug bool, ledWarning LEDController) (*Server, error) {
	s := &Server{
		logger:                         logger,
		chanWorker:                     make(chan struct{}, 2),
		dhcpService:                    defaultDhcpService,
		dnsMasqServiceDisabledForDebug: dnsMasqServiceDisabledForDebug, // hacky way of disabling dnsmasq start/stopping activity for stable network connectivity.
		ledWarning:                     ledWarning,
		// nil cfg so that it is fetched by s.GetConfig() below.
	}

	// TODO: set dynamic network adapter at startup before doing anything as a power failure will leave it in static mode
	//   and we will rely on the previous dhcp config to be valid for a force start of dnsmasq to work.

	_, err := s.GetConfig(s.logger) // GetConfig() sets the server config.
	if err != nil {
		return nil, err
	}

	if ledWarning == nil {
		return nil, fmt.Errorf("LED warning controller is nil")
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
			//   may still be running, so we advise the user via status.
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
// If the service is config disabled, then return false without an error.
// Return true if config wants dnsmasq started and the service could be started,
// i.e., there isn't already a DHCP server on the network.
// If there is a DHCP server on the network, then return false and an error.
func (s *Server) maybeStartOrStopDnsmasq(logger *zap.SugaredLogger, svc restarter) (state serviceState, err error) {
	state = s.cfg.ServiceState
	err = nil

	numAttempts := 1 // numAttempts set to 1 since we call this function repeatably from the worker anyway.
	dhcpRunningLocal := false
	dhcpRunningRouter := false
	wantEnabled := svc.isDNSMasqEnabledInConfig(s.cfg)

	defer func() {
		if state == serviceStateActive || state == serviceStateInactive { // if the service made it ALL the way up or down...
			s.cfg.needsAction = false
		}
		if s.ledWarning != nil { // TODO: stop always setting the LEDs every time maybeStartOrStopDnsmasq() is called.
			if state == serviceStateActive {
				s.ledWarning.DisableWarning()
			} else if state == serviceStateInactive {
				s.ledWarning.EnableWarning()
			}
		}
	}()

	if !s.cfg.needsAction { // if there is nothing to do...
		return
	}

	if s.dnsMasqServiceDisabledForDebug {
		state = serviceStateInactive
		logger.Infof("DNSMasq service is disabled for debug - maybeStartOrStopDnsmasq exit early")
		return
	}

	for idx := 0; idx < numAttempts; idx++ {
		// Determine DHCP service status; this only works if there is a good network.
		// Recheck below using service status.
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
				logger.Info("Waiting to stop dnsmasq (router DHCP server is not running)")
				state = serviceStateWaitingToStop
				return
			} else { // else the local dnsmasq is not running...
				state = serviceStateInactive
				return
			}
		} else { // else dnsmasq is enabled by the user...
			// We don't care if another server is running on the router.
			// Prefer to have two DHCP services running than none at all and advise the user to stop the
			// router DHCP service via web interface.

			// Start dnsmasq.
			if s.cfg.needsRestart {
				logger.Info("Attempting to (re)start dnsmasq")
				if err = svc.startDnsmasq(logger, s.cfg, s.ifaceName, s.hwAddr); err != nil { // if dnsmasq failed to started...
					logger.Errorf("Failed to start dnsmasq on attempt %d: %v", numAttempts+1, err)
					continue // retry in case of failure
				}
				s.cfg.needsRestart = false
			}

			if dhcpRunningRouter {
				state = serviceStateActiveRouterCanBeStopped
				logger.Info("Started dnsmasq (router DHCP server is still running)")
				return
			}

			state = serviceStateActive
			logger.Info("Started dnsmasq (router DHCP server is disabled OK)")
			return

			// }
		}
	}

	// All attempts failed; try to force start dnsmasq anyway.
	isLocalActive, _ := svc.isDnsmasqServiceActive()
	if wantEnabled { // if dnsmasq config is either still not enabled or it's already running...
		if isLocalActive {
			logger.Info("Restarting dnsmasq")
		} else {
			logger.Errorf("Attempting to force start dnsmasq after %v failed attempts", numAttempts)
		}
		if err = svc.startDnsmasq(logger, s.cfg, s.ifaceName, s.hwAddr); err == nil { // if dnsmasq started...
			state = serviceStateActive
			return
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

func (s *Server) GetConfig(logger *zap.SugaredLogger) (*DNSMasqConfig, error) {
	// Allow lazy mocking of the func that gets config so we don't have to mock
	// the whole inner workings of config.GetConfig in tests.
	// TODO: stop deferring to a plain func when getting/setting dnsmasq cfg - tests need updating.
	err := defaultGetConfig(logger, &s.cfg)
	if err != nil {
		return nil, err
	}
	return s.cfg, nil
}

func GetConfig(logger *zap.SugaredLogger, cfg **DNSMasqConfig) error {
	if *cfg != nil { // if dns config is already loaded...
		return nil
	}

	// Load from file.
	newCfg, err := config.GetConfig[*DNSMasqConfig](dhcpMutex, configFileDHCPSettings, newDNSMasqConfig)
	if err != nil {
		logger.Warnf("Failed to get dnsmasq config: %v", err)
		return fmt.Errorf("failed to get dnsmasq config: %w", err)
	}

	// Assume defaults if empty.
	if newCfg.DefaultGateway == nil {
		newCfg.DefaultGateway, err = getDefaultGateway()
		if err != nil {
			logger.Warnf("Failed to get default gateway: %v", err)
			return fmt.Errorf("failed to get default gateway: %w", err)
		}
	}

	ifaceName, err := getPrimaryInterfaceName()
	if err != nil {
		logger.Warnf("Failed to get primary interface (check your o/s is listed): %v", err)
		return fmt.Errorf("failed to get primary interface (check your o/s is listed): %w", err)
	}

	if newCfg.LowerBound == nil || newCfg.UpperBound == nil || newCfg.ThisGateway == nil {
		lowerBound, upperBound, err := getSubnetBoundsForInterface(ifaceName)
		if err != nil {
			logger.Warnf("Failed to get subnet range for interface %s: %v", ifaceName, err)
			return fmt.Errorf("failed to get subnet range for interface %s: %w", ifaceName, err)
		}
		newCfg.LowerBound, newCfg.UpperBound, newCfg.ThisGateway, err = adjustSubnetRange(lowerBound, upperBound, newCfg.DefaultGateway)
		if err != nil {
			logger.Warnf("Failed to adjust subnet range for interface %s: %v", ifaceName, err)
			return fmt.Errorf("failed to adjust subnet range for interface %s: %w", ifaceName, err)
		}
	}

	if len(newCfg.DnsIPs) == 0 {
		newCfg.DnsIPs = fallbackDNSIPs
	}

	// Set the package global variable to the new value.
	*cfg = newCfg

	return nil
}

func (s *Server) SetConfig(logger *zap.SugaredLogger, newCfg *DNSMasqConfig) error {
	// Allow lazy mocking of the func that gets config so we don't have to mock
	// the whole inner workings of config.GetConfig in tests.
	err := defaultSetConfig(logger, &s.cfg, newCfg)
	if err != nil {
		return err
	}
	s.restart()
	return nil
}

func SetConfig(_ *zap.SugaredLogger, oldCfg **DNSMasqConfig, newCfg *DNSMasqConfig) error {
	if oldCfg == nil {
		return fmt.Errorf("existing dnsmasq config is nil")
	}
	if newCfg == nil {
		return fmt.Errorf("supplied new dnsmasq config is nil")
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
		return nil
	}

	fnUpdateInMem := func(cfg *DNSMasqConfig) {
		cfg.needsAction = true // assume something has changed for now and that we want a restart; this should be done under lock in SetConfig().
		cfg.needsRestart = true
		*oldCfg = cfg
	}

	err := config.SetConfig[*DNSMasqConfig](dhcpMutex, configFileDHCPSettings, fnValidate, fnUpdateInMem, newCfg) // TODO: validate the incoming config but don't override any yet
	if err != nil {
		return fmt.Errorf("failed to set dnsmasq config: %w", err)
	}

	return nil
}

func (s *Server) restart() {
	s.chanWorker <- struct{}{}
}
