package group

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"example.com/youtube-nfqueue/models"
	"github.com/stretchr/testify/mock"
)

// MockManager to mock IsSrcIpGroupKnown and IsDstDomainGroupKnown
type MockManager struct {
	mock.Mock
	Manager
}

func (m *MockManager) IsSrcIpGroupKnown(srcIp models.Ip) ([]models.Group, bool) {
	args := m.Called(srcIp)
	return args.Get(0).([]models.Group), args.Bool(1)
}

func (m *MockManager) IsDstDomainGroupKnown(dstDomain models.Domain) ([]models.Group, bool) {
	args := m.Called(dstDomain)
	return args.Get(0).([]models.Group), args.Bool(1)
}

func TestIsSrcIpDestDomainKnown(t *testing.T) {
	tests := []struct {
		name                 string
		srcIp                models.Ip
		dstDomain            models.Domain
		managerModeMatchAll  bool
		mockSrcGroups        []models.Group
		mockDstGroups        []models.Group
		mockSrcOk, mockDstOk bool
		expectedGroups       []models.Group
		expectedOk           bool
	}{
		{
			name:                "Match all mode - domain known",
			srcIp:               "192.168.0.1",
			dstDomain:           "example.com",
			managerModeMatchAll: true,
			mockDstGroups:       nil,
			mockDstOk:           true,
			expectedGroups:      []models.Group{"192.168.0.1:example.com"},
			expectedOk:          true,
		},
		{
			name:                "Match all mode - domain unknown",
			srcIp:               "192.168.0.1",
			dstDomain:           "unknown.com",
			managerModeMatchAll: true,
			mockDstGroups:       nil,
			mockDstOk:           false,
			expectedGroups:      []models.Group{},
			expectedOk:          false,
		},
		{
			name:                "Intersecting groups",
			srcIp:               "192.168.0.1",
			dstDomain:           "example.com",
			managerModeMatchAll: false,
			mockSrcGroups:       []models.Group{"group1", "group2"},
			mockDstGroups:       []models.Group{"group2", "group3"},
			mockSrcOk:           true,
			mockDstOk:           true,
			expectedGroups:      []models.Group{"group2"},
			expectedOk:          true,
		},
		{
			name:                "No intersecting groups",
			srcIp:               "192.168.0.1",
			dstDomain:           "example.com",
			managerModeMatchAll: false,
			mockSrcGroups:       []models.Group{"group1"},
			mockDstGroups:       []models.Group{"group2"},
			mockSrcOk:           true,
			mockDstOk:           true,
			expectedGroups:      []models.Group{},
			expectedOk:          false,
		},
		{
			name:                "Either srcIp or dstDomain unknown",
			srcIp:               "192.168.0.1",
			dstDomain:           "example.com",
			managerModeMatchAll: false,
			mockSrcGroups:       nil,
			mockDstGroups:       nil,
			mockSrcOk:           false,
			mockDstOk:           true,
			expectedGroups:      []models.Group{},
			expectedOk:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockManager := &MockManager{}
			mockManager.On("IsSrcIpGroupKnown", tt.srcIp).Return(tt.mockSrcGroups, tt.mockSrcOk)
			mockManager.On("IsDstDomainGroupKnown", tt.dstDomain).Return(tt.mockDstGroups, tt.mockDstOk)

			managerModeMatchAllSourceIps = tt.managerModeMatchAll // set the global variable

			actualGroups, actualOk := mockManager.IsSrcIpDestDomainKnown(tt.srcIp, tt.dstDomain)

			assert.Equal(t, tt.expectedGroups, actualGroups)
			assert.Equal(t, tt.expectedOk, actualOk)

			mockManager.AssertExpectations(t)
		})
	}
}
