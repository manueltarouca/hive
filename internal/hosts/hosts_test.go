package hosts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHosts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_CONFIG", filepath.Join(dir, "config"))
	if hs, err := List(); err != nil || len(hs) != 0 {
		t.Fatalf("no file: %v, %v", hs, err)
	}
	if err := Add(Host{Label: "box", Address: "https://box.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(Host{Label: "lab", Address: "lab.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(Host{Label: "BOX", Address: "http://100.64.0.1:4483"}); err != nil {
		t.Fatal(err)
	}
	h, ok, err := Find("Box")
	if err != nil || !ok || h.Address != "http://100.64.0.1:4483" {
		t.Fatalf("Find after replacing: %+v %v %v", h, ok, err)
	}
	if hs, _ := List(); len(hs) != 2 {
		t.Fatalf("hosts = %+v, want 2", hs)
	}
	if fi, err := os.Stat(Path()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("hosts.json: %v, %v", fi.Mode(), err)
	}
	if ok, err := Remove("lab"); !ok || err != nil {
		t.Fatalf("Remove: %v %v", ok, err)
	}
	if ok, _ := Remove("lab"); ok {
		t.Fatal("removed twice")
	}
}

// A host is reached on demand unless saved to stay connected; saving it
// again (the app saves the URL a host answered at) keeps its choice.
func TestHostsConnectMode(t *testing.T) {
	t.Setenv("HIVE_CONFIG", filepath.Join(t.TempDir(), "config"))
	if err := Add(Host{Label: "lab", Address: "http://lab:4483"}); err != nil {
		t.Fatal(err)
	}
	if h, _, _ := Find("lab"); h.Connect != "" || h.Always() {
		t.Fatalf("a new host: %+v, want on demand", h)
	}
	if err := Add(Host{Label: "box", Address: "https://box.ts.net", Connect: Always}); err != nil {
		t.Fatal(err)
	}
	// Saved again without a choice: still always.
	if err := Add(Host{Label: "box", Address: "https://box.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	if h, _, _ := Find("box"); !h.Always() || h.Address != "https://box.example.ts.net" {
		t.Fatalf("re-saved: %+v, want always at the new address", h)
	}
	if err := Add(Host{Label: "box", Address: "https://box.example.ts.net", Connect: OnDemand}); err != nil {
		t.Fatal(err)
	}
	// On demand is saved as the default is: no field.
	if h, _, _ := Find("box"); h.Always() || h.Connect != "" {
		t.Fatalf("set on demand: %+v", h)
	}
	if err := Add(Host{Label: "x", Address: "http://x:1", Connect: "sometimes"}); err == nil {
		t.Fatal("an unknown connect mode was accepted")
	}
}

func TestHostsRefuse(t *testing.T) {
	t.Setenv("HIVE_CONFIG", filepath.Join(t.TempDir(), "config"))
	for _, h := range []Host{
		{Label: "", Address: "https://x.ts.net"},
		{Label: "a/b", Address: "https://x.ts.net"},
		{Label: "box.ts", Address: "https://x.ts.net"}, // would read as a host name
		{Label: "root@box", Address: "https://x.ts.net"},
		{Label: "--always", Address: "https://x.ts.net"}, // reads as a flag
		{Label: "-x", Address: "https://x.ts.net"},
		{Label: "box", Address: "https://root@x.ts.net"}, // accounts aren't supported
		{Label: "box", Address: "ftp://x"},
		{Label: "box", Address: "/tmp/x.sock"},
	} {
		if err := Add(h); err == nil {
			t.Errorf("Add(%+v) accepted", h)
		}
	}
}
