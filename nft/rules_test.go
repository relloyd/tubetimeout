package nft

import (
	"fmt"
	"testing"

	"example.com/youtube-nfqueue/models"
	"github.com/google/nftables"
	"github.com/stretchr/testify/assert"
)

func cleanupFunc() {
	err := deleteTable(&nftables.Conn{}, defaultTableName)
	fmt.Println("error during cleanup: deleteTable() error: ", err)
}

func Test_New(t *testing.T) {
	nfq, err := NewNFTRules()
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
	// t.Cleanup(cleanupFunc)

	rules, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	// Check length of chain rules.
	r, err := rules.conn.GetRules(rules.table, rules.chain)
	t.Log("num rules = ", r)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 2, len(r), "expected 2 default rules")

	// Add a single rule.
	err = rules.addNFTablesRuleForSingleDestAddr("10.20.30.1") // add any old rule
	assert.NoError(t, err, "addNFTablesRuleForSingleDestAddr() error = %v", err)
	err = rules.conn.Flush()
	assert.NoError(t, err, "Flush() error = %v", err)

	// Check length of chain rules.
	r, err = rules.conn.GetRules(rules.table, rules.chain)
	t.Log("num rules = ", r)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 3, len(r), "expected 2 default plus 1 rules = 3")
}

func Test_addNFTablesRuleForSets(t *testing.T) {
	// t.Cleanup(cleanupFunc)

	rules, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	r, err := rules.conn.GetRules(rules.table, rules.chain)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 2, len(r), "2 default rules expected")
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
		"192.168.200.100": "example.com",
		"192.168.200.101": "example.com",
	}
	rules.UpdateDestIpDomains(mid)

	// Check length of sets.
	s, err := rules.conn.GetSetElements(rules.setLocal)
	assert.Equal(t, 2, len(s), "number of IPs in set")
	for _, v := range s {
		_, ok := mig[models.Ip(v.Key)]
		assert.True(t, ok, "IP not found in local IP set")
	}

	s, err = rules.conn.GetSetElements(rules.setRemote)
	assert.Equal(t, 2, len(s), "number of IPs in set")
	for _, v := range s {
		_, ok := mid[models.Ip(v.Key)]
		assert.True(t, ok, "IP not found in remote IP set")
	}

	// TODO: test that when we add more IPs to the sets, the rules are updated and not duplicated.

	// TODO: find a way to assert the rule is using IP sets.
}

func Test_Clean(t *testing.T) {
	t.Cleanup(cleanupFunc)

	rules, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	err = rules.Clean()
	assert.NoError(t, err, "Clean() error = %v", err)

	// Check tables.
	if tableExists(rules.conn, rules.tableName) {
		t.Errorf("Table %v found when it should be gone", rules.tableName)
	}
}
