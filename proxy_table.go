package discobolt

import (
	_ "embed"
	"net"
	"strings"
)

//go:embed known_proxies.txt
var knownProxies string

type cidrItem struct {
	cidr   *net.IPNet
	header string
}

var (
	v4Items = []cidrItem{}
	v6Items = []cidrItem{}
)

// Turns the known proxies into a table.
func init() {
	for _, line := range strings.Split(knownProxies, "\n") {
		// Ignore comments or blank lines.
		if line == "" || line[0] == '#' {
			continue
		}

		// Split the line by space.
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		ipRange := parts[0]
		header := parts[1]

		// Check if this is IPv6.
		_, ipNet, err := net.ParseCIDR(ipRange)
		if err != nil {
			panic(err)
		}
		if ipNet != nil {
			if ipNet.IP.To4() == nil {
				v6Items = append(v6Items, cidrItem{ipNet, header})
			} else {
				v4Items = append(v4Items, cidrItem{ipNet, header})
			}
		}
	}
}

// Evaluates the IP and finds if it matches a known proxy. If doesn't, it returns a blank string.
func evalIp(x net.IP) string {
	// Check if this is IPv6.
	if x.To4() == nil {
		for _, item := range v6Items {
			if item.cidr.Contains(x) {
				return item.header
			}
		}
	} else {
		for _, item := range v4Items {
			if item.cidr.Contains(x) {
				return item.header
			}
		}
	}
	return ""
}
