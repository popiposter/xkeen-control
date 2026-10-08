//go:build linux

package setup

import (
	"strings"
	"testing"
)

func TestStockCommentOnlyPortsAndMalformedRanges(t *testing.T) {
	stock := "# Порты\n#80\n#443\n#596:599\n"
	if activePorts([]byte(stock)) {
		t.Fatal("comments are active ports")
	}
	found, e := excludedPort([]byte(stock))
	if e != nil || found {
		t.Fatal(e)
	}
	found, e = excludedPort([]byte(stock + "53\n"))
	if e != nil || !found {
		t.Fatal(e)
	}
	for _, s := range []string{"53\nbad", "53\n0:2", "53\n3:70000", "60:50", "no:53"} {
		if _, e := excludedPort([]byte(s)); e == nil {
			t.Fatal("malformed ports admitted")
		}
	}
}
func TestInterceptionRequiresPolicyMarkAndPriorDNSReturn(t *testing.T) {
	good := "-A PREROUTING -p tcp -m connmark --mark 0xffffad00 -m multiport --dports 53,10085 -j RETURN\n-A PREROUTING -p tcp -m connmark --mark 0xffffad00 -j xkeen\n"
	if verifyTable([]byte(good), 0xffffad00) != nil {
		t.Fatal("scoped return rejected")
	}
	for _, s := range []string{strings.Replace(good, "53,10085", "10085", 1), strings.Replace(good, "--mark 0xffffad00 -j", "--mark 0xffffaa00 -j", 1), strings.Replace(good, "-m connmark --mark 0xffffad00 -j", "-m dscp --dscp 63 -j", 1), good + "-A PREROUTING -p tcp -m dscp --dscp 61 -j xkeen_force\n"} {
		if verifyTable([]byte(s), 0xffffad00) == nil {
			t.Fatal("unsafe interception accepted")
		}
	}
}
