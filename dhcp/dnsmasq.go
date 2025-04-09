package dhcp

import (
	"context"
	"errors"
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
	serviceStop    = systemctlAction("stop")
	serviceRestart = systemctlAction("restart")

	serviceStateStarted = serviceState("active")
	serviceStateWaiting = serviceState("waiting to start")
	serviceStateStopped = serviceState("inactive")
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
	dnsMasqConfig            *DNSMasqConfig   // expect NewServer() to set dnsMasqConfig up.
	defaultDhcpService       = &dhcpService{} // allow mocking
	errDHCPServerNotRunning  = errors.New("DHCP server not running")
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
	ServiceEnabled      bool          `yaml:"serviceEnabled" json:"serviceEnabled"`
	ServiceState        serviceState  `yaml:"serviceState" json:"serviceState"`

	wantsRestart bool
}

type Reservation struct {
	MacAddr models.MAC `yaml:"macAddr" json:"macAddr"` // use string type for MacAddr so it marshals to YAML nicely - we had issues implementing interfaces to make this happen on net.HardwareAddr.
	IpAddr  net.IP     `yaml:"ipAddr" json:"ipAddr"`
	Name    string     `yaml:"name" json:"name"`
}

func newDNSMasqConfig() *DNSMasqConfig {
	return &DNSMasqConfig{
		AddressReservations: make([]Reservation, 0),
		wantsRestart:        true, // allow worker to (re)start dnsmasq for the first time
	}
}

type restarter interface {
	isDnsmasqServiceActive() (bool, error)
	isDNSMasqEnabledInConfig() bool
	isDHCPServerRunning(logger *zap.SugaredLogger, hwAddr net.HardwareAddr) (bool, error)
	setStaticIP(logger *zap.SugaredLogger, ifaceName string, cfg *DNSMasqConfig, fnFinder cidrFinderFunc) error
	unsetStaticIP(logger *zap.SugaredLogger, ifaceName string) error
	startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error
	setDnsmasqServiceState(action systemctlAction) error
}

type Server struct {
	logger      *zap.SugaredLogger
	chanWorker  chan systemctlAction
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
		chanWorker:  make(chan systemctlAction, 2),
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
	var action systemctlAction
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			dhcpMutex.Lock()
			if dnsMasqConfig.ServiceEnabled { // if service is enabled then try to bring it up...
				action = serviceRestart
			} else {
				action = serviceStop
			}
			dhcpMutex.Unlock() // don't hold the mutex while sending, as the send to channel will block if the buffer is full.
			s.chanWorker <- action
		case action := <-s.chanWorker:
			switch action {
			case serviceRestart:
				dhcpMutex.Lock()
				dnsMasqConfig.ServiceState, err = s.maybeStartDnsmasq(s.logger, s.dhcpService)
				if err != nil {
					s.logger.Errorf("Worker failed to start dnsmasq: %v", err)
				}
				dhcpMutex.Unlock()
			case serviceStop:
				err = s.Stop()
				if err != nil {
					s.logger.Errorf("Error stopping dnsmasq: %v", err)
				}
				dhcpMutex.Lock()
				dnsMasqConfig.ServiceState = serviceStateStopped
				dhcpMutex.Unlock()
			}
		}
	}
}

// maybeStartDnsmasq checks if it's okay to start dnsmasq based on config.
// If the service is config disabled then return false without an error.
// Return true if config wants dnsmasq started and the service could be started,
// i.e. there isn't already a DHCP server on the network.
// If there is a DHCP server on the network then return false and an error.
func (s *Server) maybeStartDnsmasq(logger *zap.SugaredLogger, svc restarter) (serviceState, error) {
	// Check our dnsmasq service status.
	localDnsmasqIsActive, err := svc.isDnsmasqServiceActive()
	if err != nil {
		logger.Errorf("Error while checking if dnsmasq service is active: %v", err)
		return serviceStateStopped, fmt.Errorf("error checking if dnsmasq service is active: %w", err)
	}

	// Maybe stop dnsmasq.
	if !svc.isDNSMasqEnabledInConfig() { // if dnsmasq is disabled by the user...
		if localDnsmasqIsActive {
			err := s.dhcpService.setDnsmasqServiceState(serviceStop)
			if err != nil {
				logger.Errorf("Error while stopping dnsmasq service: %v", err)
			}
		}
		return serviceStateStopped, nil
	}

	numAttempts := 5
	dhcpRunning := false

	for idx := 0; idx < numAttempts; idx++ {
		// Is dnsmasq running?
		dhcpRunning, err = svc.isDHCPServerRunning(logger, s.hwAddr)
		if err != nil && !errors.Is(err, errDHCPServerNotRunning) { // if we should try again...
			logger.Warnf("Error while checking if DHCP server is running: %v", err)
			continue
		}

		if dhcpRunning && !localDnsmasqIsActive { // if another DHCP server is running...
			// Signal that we're waiting for the other DHCP server to be stopped first.
			logger.Warn("Another DHCP server is running, waiting to start dnsmasq")
			return serviceStateWaiting, nil
		} else { // else there is no other DHCP server running, or it's our own dnsmasq service...
			if dnsMasqConfig.wantsRestart || !localDnsmasqIsActive { // if the service needs a (re)start...
				// (Re)start dnsmasq.
				pattern := "The local DHCP server %v running, attempting to (%v)start dnsmasq"
				if localDnsmasqIsActive {
					logger.Info(fmt.Sprintf(pattern, "is", ""))
				} else {
					logger.Info(fmt.Sprintf(pattern, "is not", "re"))
				}

				// Set a static IP for this gateway.
				if err := svc.setStaticIP(logger, s.ifaceName, dnsMasqConfig, findSmallestSingleCIDR); err != nil {
					logger.Warnf("Failed to set static IP on interface %s on attempt %d: %v", s.ifaceName, numAttempts+1, err)
					// TODO: surface network error to web
					continue
				}

				// Start dnsmasq.
				if err := svc.startDnsmasq(logger, dnsMasqConfig); err != nil { // if dnsmasq failed to started...
					logger.Warnf("Failed to start dnsmasq on attempt %d: %v", numAttempts+1, err)
					err := svc.unsetStaticIP(logger, s.ifaceName) // reset to dynamic IP allocation in case we need another DHCP server to get an IP for web access.
					if err != nil {
						logger.Warnf("Failed to unset static IP on interface %s on attempt %d: %v", s.ifaceName, numAttempts+1, err)
					}
					continue // retry in case of failure
				}

				logger.Info("Successfully started dnsmasq")
				return serviceStateStarted, nil
			}
		}
	}

	// All attempts failed.
	return serviceStateStopped, fmt.Errorf("failed to start dnsmasq after %d attempts", numAttempts)
}

func (s *Server) restart() {
	dnsMasqConfig.wantsRestart = true
	s.chanWorker <- serviceRestart
}

func (s *Server) Stop() error {
	return s.dhcpService.setDnsmasqServiceState(serviceStop)
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
