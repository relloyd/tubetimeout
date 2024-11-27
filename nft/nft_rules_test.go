package nft

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TODO: use a different table name for testing
// TODO: use setup and teardown functions to create and delete the table

func Test_New(t *testing.T) {
	nfq, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)
	assert.NotNil(t, nfq, "NewNFTRules() returned nil")
	assert.NotNil(t, nfq.conn, "NewNFTRules() conn is nil")
	assert.NotNil(t, nfq.table, "NewNFTRules() table is nil")
	assert.NotNil(t, nfq.chain, "NewNFTRules() chain is nil")
}

func Test_SendIP4PacketsToDefaultNFQueue(t *testing.T) {
	nfq, err := NewNFTRules()
	assert.NoError(t, err, "NewNFTRules() error = %v", err)

	// Add a single rule.
	err = nfq.addNFTablesRule("10.20.30.1") // add any old rule
	assert.NoError(t, err, "addNFTablesRule() error = %v", err)
	err = nfq.conn.Flush()
	assert.NoError(t, err, "Flush() error = %v", err)

	// Check length of chain rules.
	r, err := nfq.conn.GetRules(nfq.table, nfq.chain)
	t.Log("num rules = ", r)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 1, len(r), "expected 1 rule")

	// Add empty rules list which should cause chain flush.
	err = nfq.sendIP4PacketsToDefaultNFQueue([]string{}) // send empty IP list to cause empty rules.
	assert.NoError(t, err, "sendIP4PacketsToDefaultNFQueue() error = %v", err)

	// Check length of chain rules.
	r, err = nfq.conn.GetRules(nfq.table, nfq.chain)
	assert.NoError(t, err, "conn.GetRules() error = %v", err)
	assert.Equal(t, 0, len(r), "sendIP4PacketsToDefaultNFQueue() chain should be empty")
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
