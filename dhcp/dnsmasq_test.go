package dhcp

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
)

func TestNewServer(t *testing.T) {
	tests := []struct {
		name               string
		mockGetConfigError error
		expectServer       bool
		expectError        bool
		errorMsg           string
	}{
		{
			name:               "Successful server creation",
			mockGetConfigError: nil,
			expectServer:       true,
			expectError:        false,
			errorMsg:           "expected successful server creation without error",
		},
		{
			name:               "Failure to load DNSMasqConfig",
			mockGetConfigError: fmt.Errorf("failed to load configuration"),
			expectServer:       false,
			expectError:        true,
			errorMsg:           "expected server creation failure",
		},
	}

	originalGetConfig := defaultGetConfig
	t.Cleanup(func() {
		defaultGetConfig = originalGetConfig
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock GetConfig behavior
			defaultGetConfig = func(logger *zap.SugaredLogger) (*DNSMasqConfig, error) {
				return newDNSMasqConfig(), tt.mockGetConfigError
			}

			server, err := NewServer(context.Background(), config.MustGetLogger())

			if tt.expectError {
				assert.Error(t, err, tt.errorMsg)
			} else {
				assert.NoError(t, err, tt.errorMsg)
			}

			if tt.expectServer {
				assert.NotNil(t, server, "expected a server instance to be returned")
			} else {
				assert.Nil(t, server, "expected no server instance to be returned")
			}
		})
	}
}

func TestCheckDHCPServer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping test on non-linux platform")
	}

	ifaceName, err := getPrimaryInterfaceName()
	assert.NoError(t, err, "get primary interface name")
	iface, err := net.InterfaceByName(ifaceName)
	assert.NoError(t, err, "get interface by name")
	mac := iface.HardwareAddr
	assert.NotNil(t, mac, "No MAC address found for interface %s", ifaceName)

	// TODO: fix this non-deterministic test that uses different logic when dnsmasq is enabled vs disabled!
	//  you need to actually test isDHCPServerRunning!
	svc := &dhcpService{}
	res, err := svc.isDHCPServerRunning(config.MustGetLogger(), mac)

	if isActive, err2 := svc.isDnsmasqServiceActive(); err2 == nil && isActive {
		assert.Equal(t, true, res, "isDHCPServerRunning() should return true: %v", err)
	} else {
		assert.Equal(t, false, res, "isDHCPServerRunning() should return false: %v", err)
	}
}

// TestGetConfigCached verifies that when dnsMasqConfig is already set,
// GetConfig returns the cached configuration.
func TestGetConfigCached(t *testing.T) {
	// Prepare a dummy config.
	dummyCfg := newDNSMasqConfig()
	dummyCfg.DefaultGateway = net.ParseIP("192.168.1.1")
	// Here you can also set other fields as needed.

	// Set the global to simulate an already loaded config.
	dnsMasqConfig = dummyCfg

	// Call GetConfig. In this case, the function should simply return our dummyCfg.
	cfg, err := GetConfig(config.MustGetLogger())
	assert.NoError(t, err, "GetConfig should not return an error when a config is cached")
	assert.Equal(t, dummyCfg, cfg, "Expected cached config to be returned")
}

// TestGetConfigLoads verifies that, if dnsMasqConfig is nil,
// GetConfig attempts to load the configuration and returns a non-nil result.
func TestGetConfigLoads(t *testing.T) {
	// Reset the global to force a fresh load.
	dnsMasqConfig = nil

	// Create a temporary file for the dummy config
	tmpFile, err := os.CreateTemp("", "dnsmasq-config-*.yaml")
	assert.NoError(t, err, "Failed to create temporary file for config")
	defer os.Remove(tmpFile.Name()) // Clean up the temp file when done

	// Write dummy config data to the temporary file
	dummyConfigContent := `defaultGateway: "192.168.1.1"
thisGateway: "192.168.1.2"
lowerBound: "192.168.1.3"
upperBound: "192.168.1.254"
dnsIPs:
  - "8.8.8.8"
  - "8.8.4.4"
addressReservations:
  - macAddr: "00-00-00-00-00-00"
    ipAddr: "192.168.1.10"
    name: "test"
serviceEnabled: true
`
	_, err = tmpFile.WriteString(dummyConfigContent)
	assert.NoError(t, err, "Failed to write to temporary config file")

	// Override the global `configFileDHCPSettings` to use the temp file
	originalFile := configFileDHCPSettings
	configFileDHCPSettings = tmpFile.Name()
	defer func() {
		configFileDHCPSettings = originalFile // Restore the original value
	}()
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(fileName string) (string, error) {
		return tmpFile.Name(), nil
	}

	cfg, err := GetConfig(config.MustGetLogger())
	assert.NoError(t, err, "Expected no error when loading config")
	assert.NotNil(t, cfg, "Expected non-nil config")

	// Basic sanity check for fields that should be set.
	assert.Equal(t, net.ParseIP("192.168.1.1"), cfg.DefaultGateway, "DefaultGateway didn't match the expected value")
	assert.Equal(t, net.ParseIP("192.168.1.3"), cfg.LowerBound, "LowerBound didn't match the expected value")
	assert.Equal(t, net.ParseIP("192.168.1.254"), cfg.UpperBound, "UpperBound didn't match the expected value")
	assert.Equal(t, net.ParseIP("192.168.1.2"), cfg.ThisGateway, "ThisGateway didn't match the expected value")
	assert.Equal(t, []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("8.8.4.4")}, cfg.DnsIPs, "DNS IPs didn't match the expected value")
	assert.True(t, cfg.ServiceEnabled, "ServiceEnabled should be true")

	assert.NoError(t, err, "Expected no error when loading config")
	assert.NotNil(t, cfg, "Expected non-nil config")

	// Basic sanity check for fields that should be set.
	assert.NotNil(t, cfg.DefaultGateway, "DefaultGateway is nil")
	assert.NotNil(t, cfg.LowerBound, "LowerBound is nil")
	assert.NotNil(t, cfg.UpperBound, "UpperBound is nil")
	assert.NotNil(t, cfg.ThisGateway, "ThisGateway is nil")
}

func TestSetConfig_WritesToFile(t *testing.T) {
	originalFnDefault := config.FnDefaultCreateAppHomeDirAndGetConfigFilePath

	// Create a temporary file for the config
	tmpFile, err := os.CreateTemp("", "dnsmasq-config-*.json")
	assert.NoError(t, err)
	// Clean up the temp file when done
	defer os.Remove(tmpFile.Name())

	// Override the function to return our temp file path.
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(fileName string) (string, error) {
		return tmpFile.Name(), nil
	}
	// Restore the original function after the test completes.
	defer func() {
		config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = originalFnDefault
	}()

	// Change the global config file variable to point to the temp file.
	originalFile := configFileDNSMasqService
	configFileDNSMasqService = tmpFile.Name()
	defer func() {
		configFileDNSMasqService = originalFile
	}()

	// Create a sample config with a known value.
	sampleCfg := &DNSMasqConfig{
		DefaultGateway: net.ParseIP("192.168.1.1"),
		ThisGateway:    net.ParseIP("192.168.1.2"),
		LowerBound:     net.ParseIP("192.168.1.3"),
		UpperBound:     net.ParseIP("192.168.1.254"),
		DnsIPs:         []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("8.8.4.4")},
		AddressReservations: []Reservation{
			{MacAddr: "00:1A:2B:3C:4D:5E", IpAddr: net.ParseIP("192.168.1.10")},
		},
		ServiceEnabled: true,
	}

	// Call SetConfig and ensure no error is returned.
	err = SetConfig(config.MustGetLogger(), sampleCfg)
	assert.NoError(t, err)

	// Read back the file contents.
	b, err := os.ReadFile(tmpFile.Name())
	assert.NoError(t, err)
	content := string(b)

	// Check that the content has at least one known value from the config.
	// (For example, our DefaultGateway should be present.)
	assert.Contains(t, content, "192.168.1.1", "Expected DefaultGateway value %q to be saved in config file", "192.168.1.1")
}

func TestChooseIPFromBottom(t *testing.T) {
	tests := []struct {
		name          string
		lower         net.IP
		upper         net.IP
		expectedIP    net.IP
		expectedLower net.IP
		expectedUpper net.IP
		expectError   bool
		errorMessage  string
	}{
		{
			name:          "Valid IPv4 range, non-edge case",
			lower:         net.ParseIP("192.168.1.10"),
			upper:         net.ParseIP("192.168.1.20"),
			expectedIP:    net.ParseIP("192.168.1.10"),
			expectedLower: net.ParseIP("192.168.1.11"),
			expectedUpper: net.ParseIP("192.168.1.20"),
			expectError:   false,
			errorMessage:  "lower IP should fit normally within range",
		},
		{
			name:          "Valid IPv4 range, edge case",
			lower:         net.ParseIP("192.168.1.20"),
			upper:         net.ParseIP("192.168.1.20"),
			expectedIP:    net.ParseIP("192.168.1.20"),
			expectedLower: nil,
			expectedUpper: nil,
			expectError:   true,
			errorMessage:  "range should be exhausted when lower == upper",
		},
		{
			name:          "Invalid range, lower greater than upper",
			lower:         net.ParseIP("192.168.1.30"),
			upper:         net.ParseIP("192.168.1.20"),
			expectedIP:    nil,
			expectedLower: nil,
			expectedUpper: nil,
			expectError:   true,
			errorMessage:  "lower should not be greater than upper",
		},
		{
			name:          "Mismatched IP versions (IPv4 vs IPv6)",
			lower:         net.ParseIP("192.168.1.10"),
			upper:         net.ParseIP("2001:db8::2"),
			expectedIP:    nil,
			expectedLower: nil,
			expectedUpper: nil,
			expectError:   true,
			errorMessage:  "IP versions must match",
		},
		{
			name:          "Valid IPv6 range",
			lower:         net.ParseIP("2001:db8::1"),
			upper:         net.ParseIP("2001:db8::5"),
			expectedIP:    net.ParseIP("2001:db8::1"),
			expectedLower: net.ParseIP("2001:db8::2"),
			expectedUpper: net.ParseIP("2001:db8::5"),
			expectError:   false,
			errorMessage:  "IPv6 range should work as expected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chosenIP, newLower, newUpper, err := chooseIPFromBottom(tt.lower, tt.upper)
			if tt.expectError {
				assert.Error(t, err, tt.errorMessage)
			} else {
				assert.NoError(t, err, tt.errorMessage)
				assert.Equal(t, tt.expectedIP, chosenIP, "chosen IP is incorrect")
				assert.Equal(t, tt.expectedLower, newLower, "new lower IP is incorrect")
				assert.Equal(t, tt.expectedUpper, newUpper, "upper IP should remain unchanged")
			}
		})
	}
}

type whereToRunHack int

const (
	runOnLinux whereToRunHack = iota
	runOnMacOS
	runOnAny
)

func TestGetDefaultGateway(t *testing.T) {
	tests := []struct {
		name           string
		mockOutput     string
		mockError      error
		expectedIP     net.IP
		expectError    bool
		errorMsg       string
		whereToRunHack whereToRunHack // 0 = run on macOS; 1 = run on linux; 2 = run anywhere
	}{
		{
			name: "Valid gateway on Linux",
			mockOutput: "Kernel IP routing table\n" +
				"Destination     Gateway         Genmask         Flags Metric Ref    Use Iface\n" +
				"0.0.0.0         192.168.1.254   0.0.0.0         UG    100    0        0 eth0\n",
			mockError:      nil,
			expectedIP:     net.ParseIP("192.168.1.254"),
			expectError:    false,
			errorMsg:       "should correctly parse valid gateway",
			whereToRunHack: runOnLinux,
		},
		{
			name: "Valid gateway on macOS",
			mockOutput: "Routing tables\n" +
				"Destination     Gateway         Flags     Refs     Use    Netif\n" +
				"default         192.168.1.1     UGSc\n",
			mockError:      nil,
			expectedIP:     net.ParseIP("192.168.1.1"),
			expectError:    false,
			errorMsg:       "should correctly parse macOS gateway",
			whereToRunHack: runOnMacOS,
		},
		{
			name: "Invalid gateway parsing",
			mockOutput: "Kernel IP routing table\n" +
				"Destination     Gateway         Genmask         Flags Metric Ref    Use Iface\n" +
				"0.0.0.0         ZGFR1090       0.0.0.0         UG    100    0        0 eth0\n",
			mockError:      nil,
			expectedIP:     nil,
			expectError:    true,
			errorMsg:       "should return an error for unexpected gateway format",
			whereToRunHack: runOnAny,
		},
		{
			name:           "Command execution error",
			mockOutput:     "",
			mockError:      fmt.Errorf("execution failed"),
			expectedIP:     nil,
			expectError:    true,
			errorMsg:       "should return an error when command execution fails",
			whereToRunHack: runOnAny,
		},
		{
			name: "Missing gateway on Linux",
			mockOutput: "Kernel IP routing table\n" +
				"Destination     Gateway         Genmask         Flags Metric Ref    Use Iface\n",
			mockError:      nil,
			expectedIP:     nil,
			expectError:    true,
			errorMsg:       "should return an error when no gateway found",
			whereToRunHack: runOnAny,
		},
	}

	originalRouteCmd := routeCmd
	t.Cleanup(func() {
		routeCmd = originalRouteCmd
	})

	for _, tt := range tests {
		if tt.whereToRunHack == runOnMacOS && runtime.GOOS != "darwin" {
			t.Log("Skipping macos test on non-macOS platform: ", tt.name)
			continue
		}
		if tt.whereToRunHack == runOnLinux && runtime.GOOS != "linux" {
			t.Log("Skipping linux test on non-linux platform: ", tt.name)
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			// Mock the command execution using the new signature.
			routeCmd = func() (string, error) {
				if tt.mockError != nil {
					return "", tt.mockError
				}
				return tt.mockOutput, nil
			}

			ip, err := getDefaultGateway()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Did not expect error but got: %v. %s", err, tt.errorMsg)
				}
				if !ip.Equal(tt.expectedIP) {
					t.Errorf("Expected IP %v, got %v. %s", tt.expectedIP, ip, tt.errorMsg)
				}
			}
		})
	}
}

func TestAdjustSubnetRange(t *testing.T) {
	tests := []struct {
		name         string
		lower        string
		upper        string
		gateway      string
		expectLower  string
		expectUpper  string
		expectChosen string
		expectError  bool
	}{
		{
			name:         "Gateway outside range (below)",
			lower:        "192.168.1.1",
			upper:        "192.168.1.254",
			gateway:      "10.0.0.1",
			expectLower:  "192.168.1.1",
			expectUpper:  "192.168.1.254",
			expectChosen: "192.168.1.254",
			expectError:  false,
		},
		{
			name:         "Gateway outside range (above)",
			lower:        "192.168.1.1",
			upper:        "192.168.1.254",
			gateway:      "192.168.2.1",
			expectLower:  "192.168.1.1",
			expectUpper:  "192.168.1.254",
			expectChosen: "192.168.1.254",
			expectError:  false,
		},
		{
			name:         "Gateway equals lower",
			lower:        "192.168.1.1",
			upper:        "192.168.1.254",
			gateway:      "192.168.1.1",
			expectLower:  "192.168.1.2",
			expectUpper:  "192.168.1.254",
			expectChosen: "192.168.1.254",
			expectError:  false,
		},
		{
			name:         "Gateway equals upper",
			lower:        "192.168.1.1",
			upper:        "192.168.1.254",
			gateway:      "192.168.1.254",
			expectLower:  "192.168.1.1",
			expectUpper:  "192.168.1.253",
			expectChosen: "192.168.1.253",
			expectError:  false,
		},
		{
			name:         "Gateway in middle, larger upper segment",
			lower:        "192.168.1.1",
			upper:        "192.168.1.254",
			gateway:      "192.168.1.100",
			expectLower:  "192.168.1.101",
			expectUpper:  "192.168.1.254",
			expectChosen: "192.168.1.254",
			expectError:  false,
		},
		{
			name:         "Gateway in middle, larger lower segment",
			lower:        "192.168.1.50",
			upper:        "192.168.1.200",
			gateway:      "192.168.1.150",
			expectLower:  "192.168.1.50",
			expectUpper:  "192.168.1.149",
			expectChosen: "192.168.1.149",
			expectError:  false,
		},
		{
			name:        "No usable addresses",
			lower:       "192.168.1.1",
			upper:       "192.168.1.1",
			gateway:     "192.168.1.1",
			expectError: true,
		},
		{
			name:         "Tiny range with gateway equals upper",
			lower:        "192.168.1.1",
			upper:        "192.168.1.2",
			gateway:      "192.168.1.2",
			expectLower:  "192.168.1.1",
			expectUpper:  "192.168.1.1",
			expectChosen: "192.168.1.1",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lowerIP := net.ParseIP(tt.lower)
			upperIP := net.ParseIP(tt.upper)
			gatewayIP := net.ParseIP(tt.gateway)
			if lowerIP == nil || upperIP == nil || gatewayIP == nil {
				t.Fatalf("failed to parse one of the IP addresses")
			}

			newLower, newUpper, chosenIP, err := adjustSubnetRange(lowerIP, upperIP, gatewayIP)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !newLower.Equal(net.ParseIP(tt.expectLower)) {
				t.Errorf("expected new lower %s, got %s", tt.expectLower, newLower)
			}
			if !newUpper.Equal(net.ParseIP(tt.expectUpper)) {
				t.Errorf("expected new upper %s, got %s", tt.expectUpper, newUpper)
			}
			if !chosenIP.Equal(net.ParseIP(tt.expectChosen)) {
				t.Errorf("expected chosen IP %s, got %s", tt.expectChosen, chosenIP)
			}
		})
	}
}

func TestGetSubnetBounds_NonexistentInterface(t *testing.T) {
	// Provide a nonsense interface name to trigger an error.
	_, _, err := getSubnetBoundsForInterface("non_existent_interface")
	assert.Error(t, err, "expected error for non-existent interface")
}

func TestGetSubnetBounds_ValidInterface(t *testing.T) {
	// Try common loopback interface names; adjust as necessary for your running environment.
	candidates := []string{"lo0", "lo"}
	var ifaceName string

	for _, name := range candidates {
		if _, err := net.InterfaceByName(name); err == nil {
			ifaceName = name
			break
		}
	}

	if ifaceName == "" {
		t.Skip("No valid interface found for testing getSubnetBoundsForInterface")
	}

	lower, upper, err := getSubnetBoundsForInterface(ifaceName)
	assert.NoError(t, err, "did not expect an error for a valid interface")
	assert.NotNil(t, lower, "expected a lower bound IP")
	assert.NotNil(t, upper, "expected an upper bound IP")
	assert.NotNil(t, lower.To4(), "expected lower bound to be a valid IPv4 address")
	assert.NotNil(t, upper.To4(), "expected upper bound to be a valid IPv4 address")

	// For a subnet, the lower (network address) should be less than the upper (broadcast address)
	assert.True(t, bytes.Compare(lower, upper) < 0, "expected lower bound to be less than upper bound")
}

// TestGetHardwareAddressSuccess verifies that for at least one interface with a hardware address,
// the value returned by GetHardwareAddress matches the one from the net package.
func TestGetHardwareAddressSuccess(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("Failed to list interfaces: %v", err)
	}

	for _, iface := range interfaces {
		if len(iface.HardwareAddr) > 0 {
			hwAddr, err := getIfaceHardwareAddress(iface.Name)
			if err != nil {
				t.Errorf("Unexpected error for interface %q: %v", iface.Name, err)
			}
			expected := iface.HardwareAddr.String()
			if hwAddr.String() != expected {
				t.Errorf("Expected hardware address %q for interface %q, got %q", expected, iface.Name, hwAddr)
			}
			return // get out after one attempt.
		}
	}

	t.Skip("Skipping test; no interface with a hardware address found")
}

func TestGenerateDnsmasqConfig(t *testing.T) {
	defaultGateway := net.ParseIP("192.168.1.1")
	thisGateway := net.ParseIP("192.168.1.2")
	subnetLower := net.ParseIP("192.168.1.10")
	subnetUpper := net.ParseIP("192.168.1.100")
	thisGatewayHardwareAddr := net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}.String()
	reservations := []Reservation{
		{MacAddr: "00:00:00:00:00:00", IpAddr: net.ParseIP("192.168.1.50"), Name: "test1"},
		{MacAddr: "00:00:00:00:00:00", IpAddr: net.ParseIP("192.168.1.60"), Name: "test2"},
	}

	// namedMACs := []models.NamedMAC{
	// 	{MAC: "dc:a6:32:68:47:ea", Name: "Device1"},
	// 	{MAC: "dc:a6:32:68:47:e9", Name: ""},
	// }

	generatedConfig, err := generateDnsmasqConfig(defaultGateway, thisGateway, subnetLower, subnetUpper, thisGatewayHardwareAddr, fallbackDNSIPs, reservations)
	assert.NoError(t, err, "generateDnsmasqConfig should not return an error")

	expectedLines := []string{
		"# dnsmasq configuration generated programmatically",
		"interface=eth0",
		"dhcp-range=192.168.1.10,192.168.1.100,12h",
		"dhcp-option=option:router,192.168.1.2",
		"dhcp-option=option:dns-server,1.1.1.1,8.8.8.8",
		"no-resolv",
		"server=1.1.1.1",
		"server=8.8.8.8",
		"",
		"# static IP reservations",
		"dhcp-host=00:00:00:00:00:00,192.168.1.2 # this gateway",
		"dhcp-host=00:00:00:00:00:00,192.168.1.50 # test1",
		"dhcp-host=00:00:00:00:00:00,192.168.1.60 # test2",
	}

	// "dhcp-option=tag:customgw,option:router,192.168.1.2 # this gateway",
	// 	"dhcp-host=dc:a6:32:68:47:ea,set:customgw # Device1",
	// 	"dhcp-host=dc:a6:32:68:47:e9,set:customgw # un-named",
	// 	"",

	expectedConfig := strings.Join(expectedLines, "\n")
	if generatedConfig != expectedConfig {
		t.Errorf("expected config:\n%v\ngot:\n%v", expectedConfig, generatedConfig)
	}
}

// TestWriteDnsmasqConfig tests the writeDnsmasqConfig function.
func TestWriteDnsmasqConfig(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// Create a temporary directory for the test.
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "dnsmasq.conf")
		configContent := "test content for dnsmasq configuration"

		// Call the function under test.
		err := writeDnsmasqConfig(configPath, configContent)
		assert.NoError(t, err, "writeDnsmasqConfig should not return an error")

		// Read the file to verify its content.
		data, err := os.ReadFile(configPath)
		assert.NoError(t, err, "readDnsmasqConfig should not return an error")

		assert.Equal(t, configContent, string(data), "Content mismatch")
	})

	t.Run("failure", func(t *testing.T) {
		// Attempt to write to an invalid path (non-existing directory).
		invalidPath := "/non_existent_dir/dnsmasq.conf"
		configContent := "any content"
		err := writeDnsmasqConfig(invalidPath, configContent)
		assert.Error(t, err, "writeDnsmasqConfig should return an error when writing to an invalid path")
	})
}

func TestLenFallbackDNSIPs(t *testing.T) {
	assert.Equal(t, 2, len(fallbackDNSIPs), "fallbackDNSIPs should have 2 IPs")
}

func TestFindSmallestSingleCIDR(t *testing.T) {
	tests := []struct {
		startIP  string
		endIP    string
		expected string
	}{
		{
			startIP:  "192.168.1.10",
			endIP:    "192.168.1.100",
			expected: "192.168.1.0/25",
		},
		{
			startIP:  "10.0.0.1",
			endIP:    "10.0.0.15",
			expected: "10.0.0.0/28",
		},
		{
			startIP:  "192.168.2.0",
			endIP:    "192.168.2.255",
			expected: "192.168.2.0/24",
		},
	}

	for _, test := range tests {
		startIP := net.ParseIP(test.startIP)
		endIP := net.ParseIP(test.endIP)
		result, block := findSmallestSingleCIDR(startIP, endIP)
		b := strings.Split(result, "/")
		if len(b) != 2 {
			t.Fatalf("findSmallestSingleCIDR bad block in result: %v", block)
		}
		assert.Equal(t, test.expected, result, "findSmallestSingleCIDR %v - %v failed", test.startIP, test.endIP)
		assert.Equal(t, block, b[1], "findSmallestSingleCIDR %v - %v failed with bad block", test.startIP, test.endIP)
	}
}

//
// func TestMarshalJSON_MACAddress(t *testing.T) {
// 	tests := []struct {
// 		name            string
// 		inputMAC        MACAddress
// 		expectedOutput  string
// 		expectError     bool
// 		expectedErrText string
// 	}{
// 		{
// 			name:           "Valid MAC address",
// 			inputMAC:       MACAddress{net.HardwareAddr{0x00, 0x1A, 0x2B, 0x3C, 0x4D, 0x5E}},
// 			expectedOutput: `"00-1A-2B-3C-4D-5E"`,
// 			expectError:    false,
// 		},
// 		{
// 			name:           "Empty MAC address",
// 			inputMAC:       MACAddress{net.HardwareAddr{}},
// 			expectedOutput: `""`,
// 			expectError:    false,
// 		},
// 		{
// 			name:           "Single byte MAC address (is there a point)",
// 			inputMAC:       MACAddress{net.HardwareAddr{0xAA}},
// 			expectedOutput: `"AA"`,
// 			expectError:    false,
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			output, err := tt.inputMAC.MarshalJSON()
//
// 			if tt.expectError {
// 				assert.Error(t, err, tt.expectedErrText)
// 			} else {
// 				assert.NoError(t, err, tt.expectedErrText)
// 				assert.Equal(t, tt.expectedOutput, string(output), "unexpected JSON output for MAC address")
// 			}
// 		})
// 	}
// }
//
// func TestMACAddressMarshalYAML(t *testing.T) {
// 	tests := []struct {
// 		name         string
// 		inputMAC     string // Expected input in colon-separated format
// 		expectedText string // Expected output in hyphen-separated uppercase format
// 	}{
// 		{
// 			name:         "Basic MAC address",
// 			inputMAC:     "01:23:45:67:89:ab",
// 			expectedText: "01-23-45-67-89-AB",
// 		},
// 		{
// 			name:         "MAC address with leading zeros",
// 			inputMAC:     "0a:0b:0c:0d:0e:0f",
// 			expectedText: "0A-0B-0C-0D-0E-0F",
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			var mac MACAddress
// 			// Use UnmarshalText to initialize the MACAddress from a colon-separated string.
// 			if err := mac.UnmarshalText([]byte(tt.inputMAC)); err != nil {
// 				t.Fatalf("UnmarshalText failed: %v", err)
// 			}
//
// 			// Marshal the MAC address to text.
// 			result, err := mac.MarshalYAML()
// 			if err != nil {
// 				t.Fatalf("MarshalText returned error: %v", err)
// 			}
//
// 			if got := result; got != tt.expectedText {
// 				t.Errorf("MarshalText() = %q, want %q", got, tt.expectedText)
// 			}
// 		})
// 	}
// }
//
// func TestMACAddress_UnmarshalYAML_Valid(t *testing.T) {
// 	var mac MACAddress
//
// 	// Define a fake unmarshal function that simulates the YAML unmarshalling process.
// 	// It passes the test MAC address string to the UnmarshalYAML function.
// 	unmarshalFunc := func(v interface{}) error {
// 		// Expecting a pointer to a string.
// 		s, ok := v.(*string)
// 		if !ok {
// 			return errors.New("expected pointer to string")
// 		}
// 		// Provide a valid MAC address with hyphen separators.
// 		*s = "AA-BB-CC-DD-EE-FF"
// 		return nil
// 	}
//
// 	err := mac.UnmarshalYAML(unmarshalFunc)
// 	assert.NoError(t, err)
//
// 	// The String() method converts the MAC address to an upper-case, hyphen-separated string.
// 	expected := "AA:BB:CC:DD:EE:FF"
// 	assert.Equal(t, expected, mac.String(), "MAC address string does not match expected value")
//
// 	// Also test that providing a string with colon separators is handled correctly.
// 	var mac2 MACAddress
// 	unmarshalFuncColon := func(v interface{}) error {
// 		s, ok := v.(*string)
// 		if !ok {
// 			return errors.New("expected pointer to string")
// 		}
// 		*s = "aa:bb:cc:dd:ee:ff"
// 		return nil
// 	}
// 	err = mac2.UnmarshalYAML(unmarshalFuncColon)
// 	assert.NoError(t, err, "Expected no error for colon-separated address")
// 	assert.Equal(t, expected, mac2.String(), "MAC address string does not match expected value")
// }
