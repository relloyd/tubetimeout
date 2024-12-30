package proxy

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
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

	// Mock Request and Context
	req, _ := http.NewRequest("GET", "https://example.com/resource", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	// Mock behavior for the unhappy path
	mockGroupManager.On("IsSrcIpDestDomainKnown", models.Ip("192.168.1.100"), models.Domain("example.com")).
		Return([]models.Group{"group1", "group2"}, true)
	mockUsageTracker.On("AddSample", "group1").Return()
	mockUsageTracker.On("AddSample", "group2").Return()
	mockUsageTracker.On("HasExceededThreshold", "group1").Return(false)
	mockUsageTracker.On("HasExceededThreshold", "group2").Return(true)

	// Create the handler
	handler := GetHandler(config.MustGetLogger(), mockGroupManager, mockUsageTracker)

	// Unhappy path: threshold exceeded
	newReq, resp := handler(req, nil)

	// Assertions for the unhappy path
	assert.Nil(t, newReq, "Request should be nil when threshold is exceeded")
	assert.NotNil(t, resp, "Response should not be nil when threshold is exceeded")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "Response status code should be 403")
	body, _ := io.ReadAll(resp.Body)
	assert.True(t, strings.Contains(string(body), "Request exploded"), "Response body should contain the expected reason")

	// Verify interactions for the unhappy path
	mockGroupManager.AssertCalled(t, "IsSrcIpDestDomainKnown", models.Ip("192.168.1.100"), models.Domain("example.com"))
	mockUsageTracker.AssertCalled(t, "AddSample", "group1")
	mockUsageTracker.AssertCalled(t, "AddSample", "group2")
	mockUsageTracker.AssertCalled(t, "HasExceededThreshold", "group1")
	mockUsageTracker.AssertCalled(t, "HasExceededThreshold", "group2")

	// Mock behavior for the happy path
	mockUsageTracker.ExpectedCalls = nil // Clear previous expectations
	mockUsageTracker.On("AddSample", "group1").Return()
	mockUsageTracker.On("AddSample", "group2").Return()
	mockUsageTracker.On("HasExceededThreshold", "group1").Return(false)
	mockUsageTracker.On("HasExceededThreshold", "group2").Return(false)

	// Happy path: no thresholds exceeded
	newReq, resp = handler(req, nil)

	// Assertions for the happy path
	assert.NotNil(t, newReq, "Request should not be nil when no thresholds are exceeded")
	assert.Nil(t, resp, "Response should be nil when no thresholds are exceeded")
	assert.Equal(t, req, newReq, "Returned request should match the original request")

	// Verify interactions for the happy path
	mockGroupManager.AssertCalled(t, "IsSrcIpDestDomainKnown", models.Ip("192.168.1.100"), models.Domain("example.com"))
	mockUsageTracker.AssertCalled(t, "AddSample", "group1")
	mockUsageTracker.AssertCalled(t, "AddSample", "group2")
	mockUsageTracker.AssertCalled(t, "HasExceededThreshold", "group1")
	mockUsageTracker.AssertCalled(t, "HasExceededThreshold", "group2")

	// Verify all expectations were met
	mockGroupManager.AssertExpectations(t)
	mockUsageTracker.AssertExpectations(t)
}
