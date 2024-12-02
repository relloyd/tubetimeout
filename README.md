# youtube-nfqueue

## Useful Commands

```
# enable ip forwarding
sysctl -w net.ipv4.ip_forward=1

# generic ARP spoofing
ettercap -T -M arp:remote /<router_ip>// /<victim_ip>//

# enable ARP spoofing on my pi-zero for router at .1 address and the raspberrypi at the .53 address:
ettercap -i wlan0 -S -Tq -M arp:remote /192.168.68.1// /192.168.68.53//

```