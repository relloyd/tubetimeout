package nft

import (
	"testing"
)

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

func Test_cleanUpNFTables(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"Delete default nft table", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanUpNFTablesTable(); (err != nil) != tt.wantErr {
				t.Errorf("cleanUpNFTablesTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
