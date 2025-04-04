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

type DNSMasqConfig struct {
	DefaultGateway      net.IP        `yaml:"defaultGateway" json:"defaultGateway"`
	ThisGateway         net.IP        `yaml:"thisGateway" json:"thisGateway"`
	LowerBound          net.IP        `yaml:"lowerBound" json:"lowerBound"`
	UpperBound          net.IP        `yaml:"upperBound" json:"upperBound"`
	DnsIPs              []net.IP      `yaml:"dnsIPs" json:"dnsIPs"`
	AddressReservations []Reservation `yaml:"addressReservations" json:"addressReservations"`
	ServiceEnabled      bool          `yaml:"serviceEnabled" json:"serviceEnabled"`

	mu           *sync.Mutex
	serviceState serviceState
}

type Reservation struct {
	MacAddr models.MAC `yaml:"macAddr" json:"macAddr"` // use string type for MacAddr so it marshals to YAML nicely - we had issues implementing interfaces to make this happen on net.HardwareAddr.
	IpAddr  net.IP     `yaml:"ipAddr" json:"ipAddr"`
	Name    string     `yaml:"name" json:"name"`
}

type Server struct {
	chanWorker chan systemctlAction
	logger     *zap.SugaredLogger
}

type systemctlAction string
type serviceState string

const (
	serviceStop         = systemctlAction("stop")
	serviceStart        = systemctlAction("start")
	systemctlRestart    = systemctlAction("restart")
	serviceStateStarted = serviceState("active")
	serviceStateStopped = serviceState("inactive")
)

var (
	defaultInterfaceName     = "eth0"
	defaultLeaseDuration     = "12h"
	defaultGetConfig         = GetConfig
	defaultSetConfig         = SetConfig
	configFileDNSMasqService = "/etc/dnsmasq.conf"
	configFileDHCPSettings   = "dhcp-config.yaml"
	dnsMasqConfig            *DNSMasqConfig // expect NewServer() to set this up
	routeCmd                 = defaultRouteCmd
	routeCmdArgs             = []string{"-rn"}
	errDHCPServerNotRunning  = errors.New("DHCP server not running")
	fallbackDNSIPs           = []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")} // default DNS IPs to CloudFlare and Google
)

func newDNSMasqConfig() *DNSMasqConfig {
	return &DNSMasqConfig{
		mu:                  &sync.Mutex{},
		AddressReservations: make([]Reservation, 0),
	}
}

func NewServer(ctx context.Context, logger *zap.SugaredLogger) (*Server, error) {
	var err error

	dnsMasqConfig, err = defaultGetConfig(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get dnsmasq config: %w", err)
	}

	s := &Server{
		logger:     logger,
		chanWorker: make(chan systemctlAction),
	}

	go s.startWorker(ctx)
	s.chanWorker <- serviceStart

	return s, nil
}

func (s *Server) startWorker(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			dnsMasqConfig.mu.Lock()
			if dnsMasqConfig.ServiceEnabled {
				s.chanWorker <- serviceStart
			} else {
				s.chanWorker <- serviceStop
			}
			dnsMasqConfig.mu.Unlock()
		case action := <-s.chanWorker:
			switch action {
			case serviceStart:
				started, err := s.MaybeStartDnsmasq(s.logger)
				if err != nil {
					s.logger.Warnf("Worker failed to start dnsmasq: %v", err)
				}
				dnsMasqConfig.mu.Lock()
				if started {
					dnsMasqConfig.serviceState = serviceStateStarted
				} else {
					dnsMasqConfig.serviceState = serviceStateStopped
				}
				dnsMasqConfig.mu.Unlock()
			case serviceStop:
				err := s.Stop()
				if err != nil {
					s.logger.Errorf("Error stopping dnsmasq: %v", err)
				}
				dnsMasqConfig.mu.Lock()
				dnsMasqConfig.serviceState = serviceStateStopped
				dnsMasqConfig.mu.Unlock()
			}
		}
	}
}

// MaybeStartDnsmasq checks if it's okay to start dnsmasq based on config.
// If the service is config disabled then return false without an error.
// Return true if config wants dnsmasq started and the service could be started,
// i.e. there isn't already a DHCP server on the network.
// If there is a DHCP server on the network then return false and an error.
func (s *Server) MaybeStartDnsmasq(logger *zap.SugaredLogger) (bool, error) {
	if !isDNSMasqEnabledInConfig() {
		return false, nil
	}

	ifaceName, err := getPrimaryInterfaceName()
	if err != nil {
		return false, fmt.Errorf("failed to get primary interface: %w", err)
	}
	hwAddr, err := getIfaceHardwareAddress(ifaceName)
	if err != nil {
		return false, fmt.Errorf("failed to get hardware address for interface %s: %w", ifaceName, err)
	}

	numAttempts := 5
	dhcpRunning := false

	// Attempt to start dnsmasq.
	for idx := 0; idx < numAttempts; idx++ {
		dhcpRunning, err = isDHCPServerRunning(logger, hwAddr)
		if err != nil && !errors.Is(err, errDHCPServerNotRunning) { // if we should try again...
			logger.Warnf("Error while checking if DHCP server is running: %v", err)
			continue // proceed to the next attempt
		}

		// Check our dnsmasq service status.
		dnsmasqIsActive, err := isDnsmasqServiceActive()
		if err != nil {
			logger.Warnf("Error while checking if dnsmasq service is active: %v", err)
			continue
		}

		if dhcpRunning && !dnsmasqIsActive { // if another DHCP server is running...
			// Return an error.
			logger.Info("Another DHCP server is running, not starting dnsmasq")
			return false, fmt.Errorf("attempt to start dnsmasq when another DHCP server is already running")
		} else { // else there is no other DHCP server running, or it's our own dnsmasq service...
			if dnsmasqIsActive {
				logger.Info("The local DHCP server is running, attempting to restart dnsmasq")
			} else {
				logger.Info("DHCP server is not running, attempting to start dnsmasq")
			}

			// Set a static IP for this gateway.
			if err := setStaticIP(logger, ifaceName, dnsMasqConfig, findSmallestSingleCIDR); err != nil {
				logger.Warnf("Failed to set static IP on interface %s on attempt %d: %v", ifaceName, numAttempts+1, err)
				continue
			}

			// Start dnsmasq.
			if err := startDnsmasq(logger, dnsMasqConfig); err != nil {
				logger.Warnf("Failed to start dnsmasq on attempt %d: %v", numAttempts+1, err)
				// TODO: surface dnsmasq service errors back to the web client.
				err := unsetStaticIP(logger, ifaceName)
				if err != nil {
					logger.Warnf("Failed to unset static IP on interface %s on attempt %d: %v", ifaceName, numAttempts+1, err)
				}
				continue // retry in case of failure
			}

			logger.Info("Successfully started dnsmasq")
			return true, nil
		}
	}
	return false, fmt.Errorf("failed to start dnsmasq after %d attempts", numAttempts)
}

func (s *Server) Stop() error {
	return setDnsmasqServiceState(serviceStop)
}

func (s *Server) GetConfig(logger *zap.SugaredLogger) (*DNSMasqConfig, error) {
	return defaultGetConfig(logger)
}

func (s *Server) SetConfig(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error {
	return defaultSetConfig(logger, cfg)
}