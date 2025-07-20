// Mock implementation for testing
package dhcp

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

type mockRestarter struct {
	mock.Mock
}

func (m *mockRestarter) isDnsmasqServiceActive() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}

func (m *mockRestarter) isDNSMasqEnabledInConfig(cfg *DNSMasqConfig) bool {
	args := m.Called(cfg)
	return args.Bool(0)
}

func (m *mockRestarter) isDHCPServerRunning(logger *zap.SugaredLogger, hwAddr net.HardwareAddr) (bool, bool, error) {
	args := m.Called(logger, hwAddr)
	return args.Bool(0), args.Bool(1), args.Error(2)
}

func (m *mockRestarter) setStaticIP(logger *zap.SugaredLogger, ifaceName string, cfg *DNSMasqConfig, fnFinder cidrFinderFunc) error {
	args := m.Called(logger, ifaceName, cfg, fnFinder)
	return args.Error(0)
}

func (m *mockRestarter) unsetStaticIP(logger *zap.SugaredLogger, ifaceName string) error {
	args := m.Called(logger, ifaceName)
	return args.Error(0)
}

func (m *mockRestarter) startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig, ifaceName string, hwAddr net.HardwareAddr) error {
	args := m.Called(logger, cfg, ifaceName, hwAddr)
	return args.Error(0)
}

func (m *mockRestarter) setDnsmasqServiceState(action systemctlAction) error {
	args := m.Called(action)
	return args.Error(0)
}

func TestMaybeStartDnsmasq_SuccessfulStart(t *testing.T) {
	logger := zap.NewNop().Sugar()

	cfg := &DNSMasqConfig{ServiceEnabled: true}

	hw := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	iface := "eth0"

	s := &Server{ifaceName: iface, hwAddr: hw, logger: logger, cfg: cfg}

	// no action
	mockSvc := new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = false
	cfg.ServiceState = "mock-state"
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(false)
	state, err := s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceState("mock-state"), state)
	mockSvc.AssertExpectations(t)

	// serviceStateInactive
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = true
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(true, true, nil)
	mockSvc.On("unsetStaticIP", mock.Anything, iface).Return(nil)
	mockSvc.On("setDnsmasqServiceState", serviceStop).Return(nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateInactive, state)
	mockSvc.AssertExpectations(t)

	// serviceStateWaitingToStop
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = true
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(true, false, nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateWaitingToStop, state)
	mockSvc.AssertExpectations(t)

	// serviceStateInactive
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = true
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateInactive, state)
	mockSvc.AssertExpectations(t)

	// serviceStateRouterCanBeStopped
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = true
	cfg.needsRestart = true
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, true, nil)
	mockSvc.On("startDnsmasq", mock.Anything, cfg, iface, hw).Return(nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateActiveRouterCanBeStopped, state)
	mockSvc.AssertExpectations(t)

	// serviceStateActive
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = true
	cfg.needsRestart = true
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, nil)
	mockSvc.On("startDnsmasq", mock.Anything, cfg, iface, hw).Return(nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateActive, state)
	mockSvc.AssertExpectations(t)

	// serviceStateFailedCheckConfig and force start dnsmasq
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = true
	cfg.needsRestart = true
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, errors.New("mock error"))
	mockSvc.On("isDnsmasqServiceActive").Return(false, nil)
	mockSvc.On("startDnsmasq", mock.Anything, cfg, iface, hw).Return(nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateActive, state)
	mockSvc.AssertExpectations(t)

	// serviceStateFailedCheckConfig after running out of retries
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	cfg.needsAction = true
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, errors.New("mock error"))
	mockSvc.On("isDnsmasqServiceActive").Return(true, nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.Error(t, err)
	assert.Equal(t, serviceStateFailedCheckConfig, state)
	mockSvc.AssertExpectations(t)
}

// Test LEDController handling.

type mockLEDController struct {
	mock.Mock
}

func (m *mockLEDController) EnableWarning() {
	m.Called()
}

func (m *mockLEDController) DisableWarning() {
	m.Called()
}

func TestMaybeStartDnsmasq_LEDControllerBehavior(t *testing.T) {
	logger := zap.NewNop().Sugar()
	hw := net.HardwareAddr{0xde, 0xad, 0xbe, 0xef}
	iface := "eth0"

	// Shared config
	cfg := &DNSMasqConfig{ServiceEnabled: true, needsAction: false}

	// Case 1: Nil LEDController should not panic.

	mockSvc := new(mockRestarter)
	s := &Server{
		logger:     logger,
		cfg:        cfg,
		ifaceName:  iface,
		hwAddr:     hw,
		ledWarning: nil,
	}
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, nil)
	mockSvc.On("startDnsmasq", mock.Anything, cfg, iface, hw).Return(nil)

	_, err := s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)

	// Case 2: LEDController.DisableWarning() called when state == serviceStateActive.

	cfg = &DNSMasqConfig{ServiceEnabled: true, needsAction: false, ServiceState: serviceStateActive}
	mockLED := new(mockLEDController)
	mockLED.On("DisableWarning").Return()

	mockSvc = new(mockRestarter)
	s = &Server{
		logger:     logger,
		cfg:        cfg,
		ifaceName:  iface,
		hwAddr:     hw,
		ledWarning: mockLED,
	}
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(true)

	state, err := s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateActive, state)
	mockLED.AssertCalled(t, "DisableWarning")

	// Case 3: LEDController.EnableWarning() called when state == serviceStateInactive.

	cfg = &DNSMasqConfig{ServiceEnabled: false, needsAction: false, ServiceState: serviceStateInactive}
	mockLED = new(mockLEDController)
	mockLED.On("EnableWarning").Return()

	mockSvc = new(mockRestarter)
	s = &Server{
		logger:     logger,
		cfg:        cfg,
		ifaceName:  iface,
		hwAddr:     hw,
		ledWarning: mockLED,
	}
	mockSvc.On("isDNSMasqEnabledInConfig", cfg).Return(false)

	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateInactive, state)
	mockLED.AssertCalled(t, "EnableWarning")
}
