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
	return args.Bool(0), args.Error(2)
}

func (m *mockRestarter) isDNSMasqEnabledInConfig() bool {
	args := m.Called()
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

func (m *mockRestarter) startDnsmasq(logger *zap.SugaredLogger, cfg *DNSMasqConfig) error {
	args := m.Called(logger, cfg)
	return args.Error(0)
}

func (m *mockRestarter) setDnsmasqServiceState(action systemctlAction) error {
	args := m.Called(action)
	return args.Error(0)
}

func TestMaybeStartDnsmasq_SuccessfulStart(t *testing.T) {
	logger := zap.NewNop().Sugar()

	cfg := &DNSMasqConfig{ServiceEnabled: true}
	dnsMasqConfig = cfg

	hw := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	iface := "eth0"

	s := &Server{ifaceName: iface, hwAddr: hw, logger: logger}

	// serviceStateStopped
	mockSvc := new(mockRestarter)
	s.dhcpService = mockSvc
	mockSvc.On("isDNSMasqEnabledInConfig").Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(true, true, nil)
	mockSvc.On("unsetStaticIP", mock.Anything, iface).Return(nil)
	mockSvc.On("setDnsmasqServiceState", serviceStop).Return(nil)
	state, err := s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateStopped, state)
	mockSvc.AssertExpectations(t)

	// serviceStateWaitingToStop
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	mockSvc.On("isDNSMasqEnabledInConfig").Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(true, false, nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateWaitingToStop, state)
	mockSvc.AssertExpectations(t)

	// serviceStateStopped
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	mockSvc.On("isDNSMasqEnabledInConfig").Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateStopped, state)
	mockSvc.AssertExpectations(t)

	// serviceStateRouterCanBeStopped
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	mockSvc.On("isDNSMasqEnabledInConfig").Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, true, nil)
	mockSvc.On("setStaticIP", mock.Anything, iface, cfg, mock.AnythingOfType("cidrFinderFunc")).Return(nil)
	mockSvc.On("startDnsmasq", mock.Anything, cfg).Return(nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateRouterCanBeStopped, state)
	mockSvc.AssertExpectations(t)

	// serviceStateStarted
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	mockSvc.On("isDNSMasqEnabledInConfig").Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, nil)
	mockSvc.On("setStaticIP", mock.Anything, iface, cfg, mock.AnythingOfType("cidrFinderFunc")).Return(nil)
	mockSvc.On("startDnsmasq", mock.Anything, cfg).Return(nil)
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateActive, state)
	mockSvc.AssertExpectations(t)

	// serviceStateFailedCheckConfig after running out of retries
	mockSvc = new(mockRestarter)
	s.dhcpService = mockSvc
	mockSvc.On("isDNSMasqEnabledInConfig").Return(false)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, false, errors.New("mock error"))
	state, err = s.maybeStartOrStopDnsmasq(logger, mockSvc)
	assert.Error(t, err)
	assert.Equal(t, serviceStateFailedCheckConfig, state)
	mockSvc.AssertExpectations(t)
}
