// TODO: get TCP port filtering working for 80 and 443
// &expr.Payload{
// 	DestRegister: 2,
// 	Base:         expr.PayloadBaseNetworkHeader,
// 	Offset:       9, // Offset for protocol field in IPv4 header  // TODO: what is the offset for IPv6?
// 	Len:          1, // Length of protocol field
// },
// // Compare the extracted protocol (in register 2) with TCP
// &expr.Cmp{
// 	Op:       expr.CmpOpEq,
// 	Register: 2,         // Compare value in register 2
// 	Data:     []byte{6}, // TCP protocol number; see also gopacket/layers.IPProtocolTCP
// },
