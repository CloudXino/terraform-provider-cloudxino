package provider

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// The API stores rules in a canonical form ("80, 443" -> "80,443", "any" -> "0.0.0.0/0",
// "10.0.0.5/32" -> "10.0.0.5"). Configuration written another way would show a change on every plan,
// so Read keeps the configured text when it means the same as what the API answers.

var portItem = regexp.MustCompile(`^(\d{1,5})(?:\s*[-:]\s*(\d{1,5}))?$`)

func canonicalPorts(raw, protocol string) string {
	if strings.EqualFold(protocol, "icmp") {
		return ""
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "", "all", "*", "any", "1-65535":
		return "all"
	}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		m := portItem.FindStringSubmatch(item)
		if m == nil {
			return raw // not valid: the API will say so
		}
		lo, _ := strconv.Atoi(m[1])
		hi := lo
		if m[2] != "" {
			hi, _ = strconv.Atoi(m[2])
		}
		text := strconv.Itoa(lo)
		if hi != lo {
			text = fmt.Sprintf("%d-%d", lo, hi)
		}
		if !contains(out, text) {
			out = append(out, text)
		}
	}
	return strings.Join(out, ",")
}

func canonicalAddresses(in []string) []string {
	var out []string
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		switch strings.ToLower(a) {
		case "any", "anywhere", "all", "0.0.0.0", "0.0.0.0/0":
			return []string{"0.0.0.0/0"}
		}
		text := a
		if pfx, err := netip.ParsePrefix(a); err == nil {
			pfx = pfx.Masked()
			if pfx.Bits() == 32 {
				text = pfx.Addr().String()
			} else {
				text = pfx.String()
			}
		} else if addr, err := netip.ParseAddr(a); err == nil {
			text = addr.String()
		}
		if text == "0.0.0.0/0" {
			return []string{text}
		}
		if !contains(out, text) {
			out = append(out, text)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
