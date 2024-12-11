package proxy

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"example.com/youtube-nfqueue/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// AI generated test for function "createBlockedResponse"
func TestCreateBlockedResponse(t *testing.T) {
	// Test input
	reason := "Access Denied"

	// Call the function
	resp := createBlockedResponse(reason)

	// Assertions
	assert.NotNil(t, resp, "Response should not be nil")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "StatusCode should be 403")
	assert.Equal(t, http.StatusText(http.StatusForbidden), resp.Status, "Status should be '403 Forbidden'")
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"), "Content-Type should be 'text/plain'")
	assert.Equal(t, int64(len(reason)), resp.ContentLength, "ContentLength should match the length of the reason string")

	// Check the body
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err, "Reading response body should not produce an error")
	assert.Equal(t, reason, string(body), "Response body should match the reason string")

	// Ensure the response body is properly closed
	err = resp.Body.Close()
	assert.NoError(t, err, "Closing response body should not produce an error")
}

// Mock dependencies
type MockGroupManager struct {
	mock.Mock
}

func (m *MockGroupManager) IsSrcIpDestDomainKnown(src models.Ip, dest models.Domain) ([]models.Group, bool) {
	args := m.Called(src, dest)
	return args.Get(0).([]models.Group), args.Bool(1)
}

type MockUsageTracker struct {
	mock.Mock
}

func (m *MockUsageTracker) AddSample(group string) {
	m.Called(group)
}

func (m *MockUsageTracker) HasExceededThreshold(group string) bool {
	args := m.Called(group)
	return args.Bool(0)
}

// type MockProxyCtx struct {
// 	Req *http.Request
// }

func TestGetHandler(t *testing.T) {
	// Mock dependencies
	mockGroupManager := new(MockGroupManager)
	mockUsageTracker := new(MockUsageTracker)
	req, _ := http.NewRequest("GET", "https://example.com/resource", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	// ctx := &MockProxyCtx{Req: req}

	// Mock behavior
	mockGroupManager.On(
		"IsSrcIpDestDomainKnown",
		models.Ip("192.168.1.100"),
		models.Domain("example.com")).
		Return([]models.Group{"group1", "group2"}, true)
	mockUsageTracker.On("AddSample", "group1").Return()
	mockUsageTracker.On("AddSample", "group2").Return()
	mockUsageTracker.On("HasExceededThreshold", "group1").Return(false)
	mockUsageTracker.On("HasExceededThreshold", "group2").Return(true)

	// Create the handler
	handler := GetHandler(mockGroupManager, mockUsageTracker)

	// Call the handler
	newReq, resp := handler(req, nil)

	// Assertions
	assert.Nil(t, newReq, "Request should be nil when threshold is exceeded")
	assert.NotNil(t, resp, "Response should exist when threshold is exceeded")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "Response status code should be 403")
	body, _ := io.ReadAll(resp.Body)
	assert.True(t, strings.Contains(string(body), "Request exploded"), "Response body should contain the expected reason")

	// Verify interactions
	mockGroupManager.AssertExpectations(t)
	mockUsageTracker.AssertExpectations(t)
}
