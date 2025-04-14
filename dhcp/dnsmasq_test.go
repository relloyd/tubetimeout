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
	return args.Bool(0), args.Bool(1), args.Error(1)
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
	mockSvc := new(mockRestarter)
	logger := zap.NewNop().Sugar()

	cfg := &DNSMasqConfig{ServiceEnabled: true}
	dnsMasqConfig = cfg

	hw := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	iface := "eth0"

	// serviceStateStarted
	mockSvc.On("isDnsmasqServiceActive").Return(false, nil)
	mockSvc.On("isDNSMasqEnabledInConfig").Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(false, nil)
	mockSvc.On("setStaticIP", mock.Anything, iface, cfg, mock.AnythingOfType("cidrFinderFunc")).Return(nil)
	mockSvc.On("startDnsmasq", mock.Anything, cfg).Return(nil)

	s := &Server{ifaceName: iface, hwAddr: hw}
	state, err := s.maybeStartDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateStarted, state)
	mockSvc.AssertExpectations(t)

	// serviceStateWaiting
	mockSvc = new(mockRestarter)
	mockSvc.On("isDnsmasqServiceActive").Return(false, nil)
	mockSvc.On("isDNSMasqEnabledInConfig").Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(true, nil)
	state, err = s.maybeStartDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateWaiting, state)
	mockSvc.AssertExpectations(t)

	// serviceStateStopped
	mockSvc = new(mockRestarter)
	mockSvc.On("isDnsmasqServiceActive").Return(false, errors.New("mock error"))
	state, err = s.maybeStartDnsmasq(logger, mockSvc)
	assert.Error(t, err)
	assert.Equal(t, serviceStateStopped, state)
	mockSvc.AssertExpectations(t)

	// serviceStateStopped
	mockSvc = new(mockRestarter)
	mockSvc.On("isDnsmasqServiceActive").Return(false, nil)
	mockSvc.On("isDNSMasqEnabledInConfig").Return(false)
	state, err = s.maybeStartDnsmasq(logger, mockSvc)
	assert.NoError(t, err)
	assert.Equal(t, serviceStateStopped, state)
	mockSvc.AssertExpectations(t)

	// serviceStateStopped after running out of retries
	mockSvc = new(mockRestarter)
	mockSvc.On("isDnsmasqServiceActive").Return(false, nil)
	mockSvc.On("isDNSMasqEnabledInConfig").Return(true)
	mockSvc.On("isDHCPServerRunning", mock.Anything, hw).Return(true, errors.New("mock error"))
	state, err = s.maybeStartDnsmasq(logger, mockSvc)
	assert.Error(t, err)
	assert.Equal(t, serviceStateStopped, state)
	mockSvc.AssertExpectations(t)
}
