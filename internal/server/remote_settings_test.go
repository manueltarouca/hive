package server

import (
	"testing"

	"github.com/manueltarouca/hive/internal/config"
	"github.com/manueltarouca/hive/internal/remote"
)

// The settings file's tailscale = true starts the built-in node unless
// --tailscale already did, keeps the options' policy and listeners, and
// listen still applies beside it.
func TestRemoteFromSettings(t *testing.T) {
	if !remote.Built {
		t.Skip("built without Tailscale")
	}
	conf := config.Defaults()
	if rc := remoteFromSettings(nil, conf); rc != nil {
		t.Fatalf("nothing asked, got %+v", rc)
	}
	conf.Tailscale, conf.TailscaleHostname = true, "box"
	rc := remoteFromSettings(nil, conf)
	if rc == nil || rc.Node == nil || rc.Node.Hostname != "box" || rc.Policy != (remote.Policy{}) || !rc.NodeOptional {
		t.Fatalf("tailscale = true: %+v", rc)
	}
	conf.Listen = []string{"tailnet://:4483"}
	rc = remoteFromSettings(nil, conf)
	if rc.Node == nil || len(rc.Listen) != 1 || !rc.Optional {
		t.Fatalf("with listen: %+v", rc)
	}
	given := &RemoteConfig{Policy: remote.Policy{AllowMembers: true}, Node: &remote.NodeOptions{Hostname: "flag"}}
	rc = remoteFromSettings(given, conf)
	if rc.Node.Hostname != "flag" || !rc.Policy.AllowMembers || rc.NodeOptional {
		t.Fatalf("--tailscale wins: %+v", rc)
	}
}
