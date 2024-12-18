package nft

import (
	"testing"

	"example.com/youtube-nfqueue/models"
	"github.com/stretchr/testify/assert"
)

func Test_New(t *testing.T) {
	nfq, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)
	assert.NotNil(t, nfq, "NewNFTRules() returned nil")
	assert.NotNil(t, nfq.conn, "NewNFTRules() conn is nil")
	assert.NotNil(t, nfq.table, "NewNFTRules() table is nil")
	assert.NotNil(t, nfq.chain, "NewNFTRules() chain is nil")
}

func Test_addNFTablesRuleForSingleDestAddr(t *testing.T) {
	rules, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	// Add a single rule.
	err = rules.addNFTablesRuleForSingleDestAddr("10.20.30.1") // add any old rule
	assert.NoError(t, err, "addNFTablesRuleForSingleDestAddr() error = %v", err)
	err = rules.conn.Flush()
	assert.NoError(t, err, "Flush() error = %v", err)

	// Check length of chain rules.
	r, err := rules.conn.GetRules(rules.table, rules.chain)
	t.Log("num rules = ", r)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 1, len(r), "expected 1 rule")
}

func Test_addNFTablesRuleForSets(t *testing.T) {
	rules, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	r, err := rules.conn.GetRules(rules.table, rules.chain)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 0, len(r), "no rules should exist because we haven't got src and dest IPs yet")

	// Set up source and destination IPs.
	rules.UpdateSourceIpGroups(models.MapIpGroups{
		"192.168.100.100": {"exampleGroup"},
		"192.168.100.101": {"exampleGroup"},
	})

	rules.UpdateDestIpDomains(models.MapIpDomain{
		"192.168.200.100": "example.com",
		"192.168.200.101": "example.com",
	})

	// Trigger rule generation.
	err = rules.updateIpSets()
	assert.NoError(t, err, "updateIpSets() error = %v", err)

	// Check length of chain rules.
	r, err = rules.conn.GetRules(rules.table, rules.chain)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 2, len(r), "updateIpSets() should create rules")

	for _, v := range r {
		assert.Equal(t, rules.table, v.Table, "rule created for unexpected table")
		assert.Equal(t, rules.chain, v.Chain, "rule created for unexpected chain")
	}

	// TODO: find a way to assert the rule is using IP sets.
}

func Test_Clean(t *testing.T) {
	nfq, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	err = nfq.Clean()
	assert.NoError(t, err, "Clean() error = %v", err)

	// Check tables.
	if tableExists(nfq.conn, nfq.tableName) {
		t.Errorf("Table %v found when it should be gone", nfq.tableName)
	}
}
