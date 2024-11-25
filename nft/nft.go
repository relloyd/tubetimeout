package nft

import (
	"errors"
	"fmt"
	"net"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

var (
	defaultTableName = "crazydeer-table"
)

func addNFTablesRule(ipString string) error {
	// # iptables -I OUTPUT -p icmp -j NFQUEUE --queue-num 100
	// Example YouTube IP address
	ip := net.ParseIP(ipString)
	if ip == nil {
		return errors.New("invalid IP address")
	}

	// Initialize a new nftables connection
	conn := &nftables.Conn{}

	// Create a table for inet family (IPv4/IPv6)
	table := &nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   defaultTableName,
	}
	conn.AddTable(table)

	// Create a FILTER chain in the table
	chain := &nftables.Chain{
		Name:     defaultTableName,
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookForward,    // use Forward hook to capture packets routed through the system.
		Priority: nftables.ChainPriorityFilter, // use ChainPriorityFilter which is the default filter chain priority 0
	}
	conn.AddChain(chain)

	// Add a rule to send traffic to NFQUEUE
	rule := &nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			// Match destination IP address
			&expr.Payload{
				DestRegister: 1,                             // Store the payload in register 1
				Base:         expr.PayloadBaseNetworkHeader, // Match the network header
				Offset:       16,                            // Offset for IPv4 destination address
				Len:          4,                             // Length of an IPv4 address
				// TODO: handle IPv6 in nftables rules
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     ip.To4(), // Match the specific IPv4 address
			},
			// Send matched packets to NFQUEUE
			&expr.Queue{
				Num:   100, // NFQUEUE number
				Total: 1,   // Single queue
				Flag:  expr.QueueFlagBypass,
			},
		},
	}
	conn.AddRule(rule)

	// Flush changes to the kernel
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftables rules: %v", err)
	}

	return nil
}

func cleanUpNFTablesTable() error {
	// Initialize a new nftables connection
	conn := &nftables.Conn{}

	// Delete the table and all its chains and rules
	conn.DelTable(&nftables.Table{Name: defaultTableName})

	tables, err := conn.ListTables()
	if err != nil {
		return fmt.Errorf("failed to list nft tables: %v", err)
	}

	err = conn.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush nft: %v", err)
	}

	for _, v := range tables {
		if v.Name == "" {
			return fmt.Errorf("nft table %q not deleted", defaultTableName)
		}
	}

	return nil
}
