package nft

import (
	"testing"

	"github.com/google/nftables"
	"github.com/stretchr/testify/assert"
)

// TODO: use a different table name for testing
// TODO: use setup and teardown functions to create and delete the table

func Test_CleanAll(t *testing.T) {
	t.Run("delete table", func(t *testing.T) {
		conn := &nftables.Conn{}
		table, err := getOrCreateTable(conn, defaultTableName)
		assert.NoError(t, err, "getOrCreateTable() error = %v", err)
		err = deleteTable(conn, table.Name)
		assert.NoError(t, err, "CleanAll() error = %v, wantErr %v", err, tt.wantErr)
	})
}

func Test_getOrCreateDefaultTable(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"Create default nft table", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getOrCreateDefaultTable()
			if (err != nil) != tt.wantErr {
				t.Errorf("getOrCreateTable() error = %v, wantErr %v", err, tt.wantErr)
			}
			err = conn.Flush()
			if (err != nil) != tt.wantErr {
				t.Errorf("conn.Flush() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tableExists(defaultTableName) {
				t.Errorf("Table %s does not exist", defaultTableName)
			}
		})
	}
}

func Test_addNFTablesRule(t *testing.T) {
	type args struct {
		ipString string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"Add nft rule for www.youtube.com", args{"142.250.179.238"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := addNFTablesRule(tt.args.ipString); (err != nil) != tt.wantErr {
				t.Errorf("addNFTablesRule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_SendIP4PacketsToDefaultNFQueue(t *testing.T) {
	// Add a single rule.
	err := addNFTablesRule("10.20.30.1") // add any old rule
	if err != nil {
		t.Errorf("addNFTablesRule() error = %v", err)
	}
	// Add empty rules list which should cause chain flush.
	err = SendIP4PacketsToDefaultNFQueue([]string{""}) // send empty IP list to cause empty rules.
	if err == nil {
		t.Errorf("SendIP4PacketsToDefaultNFQueue() missing error")
	}
	// Check length of chain rules.
	r, err := conn.GetRules(table, chain)
	if err != nil {
		t.Errorf("conn.GetRules() error = %v", err)
	}
	if len(r) != 0 {
		t.Errorf("SendIP4PacketsToDefaultNFQueue() chain rules not empty")
	}
}
