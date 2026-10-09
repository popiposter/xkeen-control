//go:build linux

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshRefusesDeletedComponentsAndUnknownProcReadback(t *testing.T) {
	root := t.TempDir()
	if noRunningComponents(root, 1) != nil {
		t.Fatal("empty synthetic process set")
	}
	dir := filepath.Join(root, "222")
	if os.Mkdir(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "comm"), []byte("synthetic\n"), 0600) != nil || os.Symlink("/opt/sbin/xray (deleted)", filepath.Join(dir, "exe")) != nil {
		t.Fatal("fixture")
	}
	if noRunningComponents(root, 1) == nil {
		t.Fatal("deleted running component admitted")
	}
	if noRunningComponents(filepath.Join(root, "unavailable"), 1) == nil {
		t.Fatal("unknown process view was absence")
	}
}

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
	stock := strings.ReplaceAll(good, "-m connmark", "-m conntrack ! --ctstate INVALID -m connmark")
	stock = strings.ReplaceAll(stock, "-j", "-m comment --comment \"xkeen_rule\" -j")
	if verifyTable([]byte(stock), 0xffffad00) != nil {
		t.Fatal("stock conntrack negation rejected")
	}
	if protocols, err := scopedTable([]byte(stock), 0xffffad00); err != nil || protocols != 1 {
		t.Fatal("TCP-only rules claimed UDP coverage")
	}
	if protocols, err := scopedTable([]byte(stock+strings.ReplaceAll(stock, "tcp", "udp")), 0xffffad00); err != nil || protocols != 3 {
		t.Fatal("stock TCP/UDP coverage rejected")
	}
	for _, s := range []string{
		strings.Replace(good, "53,10085", "10085", 1),
		strings.Replace(good, "--mark 0xffffad00 -j", "--mark 0xffffaa00 -j", 1),
		strings.Replace(good, "-m connmark --mark 0xffffad00 -j", "-m dscp --dscp 63 -j", 1),
		good + "-A PREROUTING -p tcp -m dscp --dscp 61 -j xkeen_force\n",
		strings.Replace(good, "--mark 0xffffad00 -j", "! --mark 0xffffad00 -j", 1),
		strings.Replace(good, "--dports", "! --dports", 1),
		strings.Replace(good, "--mark", "! --mark", 1),
		strings.Replace(good, "-j xkeen", "-s 192.0.2.1 -j xkeen", 1),
		strings.Replace(good, "-j RETURN", "-i eth0 -j RETURN", 1),
		strings.Replace(good, "-j xkeen", "-m multiport --dports 443 -j xkeen", 1),
		strings.Replace(good, "--mark 0xffffad00 -j", "--mark 0xffffad00/0xffffff00 -j", 1),
	} {
		if verifyTable([]byte(s), 0xffffad00) == nil {
			t.Fatal("unsafe interception accepted")
		}
	}
}

func TestSchemaTwoStartupNeedsPanelButRefusesDNSArtifacts(t *testing.T) {
	root := t.TempDir()
	initDir := filepath.Join(root, "etc/init.d")
	if err := os.MkdirAll(initDir, 0700); err != nil {
		t.Fatal(err)
	}
	panel := []byte("#!/bin/sh\n\"$BIN\" setup guard\n")
	if err := os.WriteFile(filepath.Join(initDir, "S99xkeen-control"), panel, 0700); err != nil {
		t.Fatal(err)
	}
	if err := startupFilesAt(root, false); err != nil {
		t.Fatal(err)
	}
	if err := startupFilesAt(root, true); err == nil {
		t.Fatal("legacy DNS requirement skipped")
	}
	if err := os.WriteFile(filepath.Join(initDir, "S06mosdns"), []byte(dnsInit), 0700); err != nil {
		t.Fatal(err)
	}
	if err := startupFilesAt(root, false); err == nil {
		t.Fatal("foreign DNS autostart admitted")
	}
	if err := startupFilesAt(root, true); err != nil {
		t.Fatal("legacy startup changed", err)
	}
}
