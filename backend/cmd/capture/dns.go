package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/publicsuffix"
)

// DNS evidence is collected with direct protocol queries (not the OS stub
// resolver) so every record keeps its TTL and the answering resolver is known.

type dnsRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	TTL   uint32 `json:"ttl"`
	Value string `json:"value"`
}

type dnsQuery struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Rcode   string      `json:"rcode"`
	Records []dnsRecord `json:"records"`
	Error   string      `json:"error,omitempty"`
}

type dnsInfo struct {
	Resolver  string    `json:"resolver"`
	QueriedAt time.Time `json:"queried_at"`
	// Hosts are every host seen in the navigation: requested, redirects, final.
	Hosts             []string   `json:"hosts"`
	RegisteredDomains []string   `json:"registered_domains"`
	Queries           []dnsQuery `json:"queries"`
}

var (
	hostTypes   = []uint16{dns.TypeA, dns.TypeAAAA, dns.TypeCNAME, dns.TypeTXT}
	domainTypes = []uint16{dns.TypeA, dns.TypeAAAA, dns.TypeNS, dns.TypeMX, dns.TypeTXT, dns.TypeSOA, dns.TypeCAA}
)

func resolverAddr() string {
	if s := os.Getenv("CAPTURE_DNS_SERVER"); s != "" {
		if _, _, err := net.SplitHostPort(s); err != nil {
			s = net.JoinHostPort(s, "53")
		}
		return s
	}
	if cfg, err := dns.ClientConfigFromFile("/etc/resolv.conf"); err == nil && len(cfg.Servers) > 0 {
		return net.JoinHostPort(cfg.Servers[0], cfg.Port)
	}
	return "1.1.1.1:53"
}

func exchange(ctx context.Context, server, name string, qtype uint16) dnsQuery {
	q := dnsQuery{Name: dns.Fqdn(name), Type: dns.TypeToString[qtype], Records: []dnsRecord{}}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = true
	m.SetEdns0(4096, false)

	c := &dns.Client{Timeout: 4 * time.Second}
	r, _, err := c.ExchangeContext(ctx, m, server)
	if err == nil && r.Truncated {
		c.Net = "tcp"
		r, _, err = c.ExchangeContext(ctx, m, server)
	}
	if err != nil {
		q.Error = err.Error()
		return q
	}
	q.Rcode = dns.RcodeToString[r.Rcode]
	for _, rr := range r.Answer {
		h := rr.Header()
		q.Records = append(q.Records, dnsRecord{
			Name:  h.Name,
			Type:  dns.TypeToString[h.Rrtype],
			TTL:   h.Ttl,
			Value: strings.TrimSpace(strings.TrimPrefix(rr.String(), h.String())),
		})
	}
	return q
}

// collectDNS queries every host in the navigation (requested URL, redirect
// hops, final URL), their registered domains, and reverse DNS for every
// address involved, including the IP the browser actually connected to.
func collectDNS(parent context.Context, hosts []string, connectedIP string) dnsInfo {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer cancel()

	server := resolverAddr()
	info := dnsInfo{Resolver: server, QueriedAt: time.Now().UTC(), Hosts: []string{}, RegisteredDomains: []string{}, Queries: []dnsQuery{}}
	seen := map[string]bool{}
	run := func(name string, t uint16) {
		key := strings.ToLower(dns.Fqdn(name)) + "/" + dns.TypeToString[t]
		if seen[key] {
			return
		}
		seen[key] = true
		if t == dns.TypePTR {
			info.Queries = append(info.Queries, ptrQuery(ctx, server, name))
			return
		}
		info.Queries = append(info.Queries, exchange(ctx, server, name, t))
	}

	ips := map[string]bool{}
	if connectedIP != "" {
		ips[strings.Trim(connectedIP, "[]")] = true
	}
	hostSet := map[string]bool{}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSuffix(h, "."))
		if h == "" || hostSet[h] {
			continue
		}
		hostSet[h] = true
		info.Hosts = append(info.Hosts, h)
		if ip := net.ParseIP(h); ip != nil {
			ips[ip.String()] = true
			continue
		}
		for _, t := range hostTypes {
			run(h, t)
		}
		if d, err := publicsuffix.EffectiveTLDPlusOne(h); err == nil {
			if !slices.Contains(info.RegisteredDomains, d) {
				info.RegisteredDomains = append(info.RegisteredDomains, d)
			}
			for _, t := range domainTypes {
				run(d, t)
			}
		}
	}
	for _, q := range info.Queries {
		if !hostSet[strings.TrimSuffix(strings.ToLower(q.Name), ".")] {
			continue
		}
		for _, r := range q.Records {
			if r.Type == "A" || r.Type == "AAAA" {
				ips[r.Value] = true
			}
		}
	}
	sorted := make([]string, 0, len(ips))
	for ip := range ips {
		sorted = append(sorted, ip)
	}
	slices.Sort(sorted)
	for _, ip := range sorted {
		run(ip, dns.TypePTR)
	}
	return info
}

func ptrQuery(ctx context.Context, server, ip string) dnsQuery {
	arpa, err := dns.ReverseAddr(ip)
	if err != nil {
		return dnsQuery{Name: ip, Type: "PTR", Records: []dnsRecord{}, Error: err.Error()}
	}
	return exchange(ctx, server, arpa, dns.TypePTR)
}

// zoneText renders the DNS evidence in a dig/zone-file style that reads well
// in court bundles and diffs cleanly between captures.
func zoneText(info dnsInfo, requestedURL string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "; Lumen DNS capture\n")
	fmt.Fprintf(&b, "; Captured URL:      %s\n", requestedURL)
	fmt.Fprintf(&b, "; Hosts:             %s\n", strings.Join(info.Hosts, ", "))
	fmt.Fprintf(&b, "; Registered domain: %s\n", strings.Join(info.RegisteredDomains, ", "))
	fmt.Fprintf(&b, "; Resolver:          %s\n", info.Resolver)
	fmt.Fprintf(&b, "; Queried at:        %s\n", info.QueriedAt.Format(time.RFC3339))
	for _, q := range info.Queries {
		status := q.Rcode
		if q.Error != "" {
			status = "ERROR " + q.Error
		} else if len(q.Records) == 0 {
			status += ", no records"
		}
		fmt.Fprintf(&b, "\n;; %s %s  (%s)\n", q.Name, q.Type, status)
		for _, r := range q.Records {
			fmt.Fprintf(&b, "%-40s %6d IN %-6s %s\n", r.Name, r.TTL, r.Type, r.Value)
		}
	}
	return []byte(b.String())
}
