package nft

import (
	"fmt"
	"net"
	"testing"

	"github.com/google/nftables"
	"github.com/stretchr/testify/assert"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

func cleanupFunc() {
	err := deleteTable(config.MustGetLogger(), &nftables.Conn{}, defaultTableName)
	fmt.Println("error during cleanup: deleteTable() error: ", err)
}

func Test_New(t *testing.T) {
	t.Cleanup(cleanupFunc)
	defaultTableName = "test_table"
	nfq, err := NewNFTRules(config.MustGetLogger(), &config.FilterConfig{})
	assert.NoError(t, err, "NewNFTRules() error = %v", err)
	assert.NotNil(t, nfq, "NewNFTRules() returned nil")
	assert.NotNil(t, nfq.conn, "NewNFTRules() conn is nil")
	assert.NotNil(t, nfq.table, "NewNFTRules() table is nil")
	assert.NotNil(t, nfq.chain, "NewNFTRules() chain is nil")
	assert.NotNil(t, nfq.setLocal, "NewNFTRules() setLocal is nil")
	assert.NotNil(t, nfq.setRemote, "NewNFTRules() setRemote is nil")
	assert.Equal(t, nfq.nameSetLocal, defaultSrcIpSetName, "NewNFTRules() nameSetLocal is bad")
	assert.Equal(t, nfq.nameSetRemote, defaultDestIpSetName, "NewNFTRules() nameSetRemote is bad")
}

func Test_addNFTablesRuleForSingleDestAddr(t *testing.T) {
	t.Cleanup(cleanupFunc)
	defaultTableName = "test_table"

	rules, err := NewNFTRules(config.MustGetLogger(), &config.FilterConfig{})
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	// Check length of chain rules.
	r, err := rules.conn.GetRules(rules.table, rules.chain)
	t.Log("num rules = ", r)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 4, len(r), "expected 4 default rules") // 2 src-dest rules; 2 udp blocking rules

	// Add a single rule.
	err = rules.addNFTablesRuleForSingleDestAddr("10.20.30.1") // add any old rule
	assert.NoError(t, err, "addNFTablesRuleForSingleDestAddr() error = %v", err)
	err = rules.conn.Flush()
	assert.NoError(t, err, "Flush() error = %v", err)

	// Check length of chain rules.
	r, err = rules.conn.GetRules(rules.table, rules.chain)
	t.Log("num rules = ", r)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 5, len(r), "expected 3 default plus 1 rules = 4") // 2 src-dest rules; 2 udp blocking rules; 1 new rule
}

func Test_addNFTablesRuleForSets(t *testing.T) {
	t.Cleanup(cleanupFunc)
	defaultTableName = "test_table"

	rules, err := NewNFTRules(config.MustGetLogger(), &config.FilterConfig{})
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	r, err := rules.conn.GetRules(rules.table, rules.chain)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 4, len(r), "4 default rules expected") // 2 src-dest rules; 2 udp blocking rule
	for _, v := range r {
		assert.Equal(t, rules.tableName, v.Table.Name, "rule created for unexpected table")
		assert.Equal(t, rules.chainName, v.Chain.Name, "rule created for unexpected chain")
	}

	// Set up source and destination IPs.
	mig := models.MapIpGroups{
		"192.168.100.100": {"exampleGroup"},
		"192.168.100.101": {"exampleGroup"},
	}
	rules.UpdateSourceIpGroups(mig)

	mid := models.MapIpDomain{
		"192.168.100.102": "example.com",
		"192.168.100.103": "example.com",
	}
	rules.UpdateDestIpDomains(mid)

	// Check length of local set.
	elem, err := rules.conn.GetSetElements(rules.setLocal)
	assert.NoError(t, err, "local rule set error = %v", err)
	assert.Equal(t, 2, len(elem), "number of IPs in set")
	for _, e := range elem {
		_, ok := mig[models.Ip(net.IP(e.Key).String())]
		assert.True(t, ok, "IP not found in local IP set")
	}

	// Check length of remote set.
	elem, err = rules.conn.GetSetElements(rules.setRemote)
	assert.NoError(t, err, "remote rule set error = %v", err)
	assert.Equal(t, 2, len(elem), "number of IPs in set")
	for _, e := range elem {
		_, ok := mid[models.Ip(net.IP(e.Key).String())]
		assert.True(t, ok, "IP not found in remote IP set")
	}

	// Test that when we add more IPs to the sets, the rules are fully replaced.
	rules.localIPs = []nftables.SetElement{{Key: net.ParseIP("192.168.200.100").To4()}}
	rules.remoteIPs = []nftables.SetElement{{Key: net.ParseIP("192.168.200.101").To4()}}
	err = rules.updateIpSets()
	assert.NoError(t, err, "updateIpSets() error = %v", err)

	elem, err = rules.conn.GetSetElements(rules.setLocal)
	assert.NoError(t, err, "local rule set error = %v", err)
	assert.Equal(t, 1, len(elem), "number of IPs in local set")

	elem, err = rules.conn.GetSetElements(rules.setRemote)
	assert.NoError(t, err, "remote rule set error = %v", err)
	assert.Equal(t, 1, len(elem), "number of IPs in remote set")

	// TODO: find a way to assert the rule is using IP sets.
}

func Test_Clean(t *testing.T) {
	t.Cleanup(cleanupFunc)
	defaultTableName = "test_table"

	logger := config.MustGetLogger()

	rules, err := NewNFTRules(logger, &config.FilterConfig{})
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	err = rules.Clean(logger)
	assert.NoError(t, err, "Clean() error = %v", err)

	// Check tables.
	if tableExists(logger, rules.conn, rules.tableName) {
		t.Errorf("Table %v found when it should be gone", rules.tableName)
	}
}
