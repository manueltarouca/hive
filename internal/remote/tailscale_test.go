//go:build !notsnet

package remote

import (
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

func TestPeerOf(t *testing.T) {
	self := &ipnstate.PeerStatus{ID: "self-node"}
	alice := &tailcfg.UserProfile{LoginName: "alice@github"}
	// A process on this machine connecting to its own tailnet address is
	// answered with this node, and its owner: never let that in.
	if _, err := peerOf(&apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: "self-node", Name: "box.ts.net."}, UserProfile: alice}, self); err == nil {
		t.Fatal("this node itself was accepted as a peer")
	}
	p, err := peerOf(&apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: "laptop", Name: "laptop.ts.net."}, UserProfile: alice}, self)
	if err != nil || p.Login != "alice@github" || p.Node != "laptop.ts.net" || p.Principal() != "tailscale:alice@github" {
		t.Fatalf("a person's device: %+v, %v", p, err)
	}
	// A tagged node arrives with the tagged-devices profile: known by its
	// tags only.
	p, err = peerOf(&apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: "ci", Tags: []string{"tag:ci"}},
		UserProfile: &tailcfg.UserProfile{LoginName: "tagged-devices"}}, self)
	if err != nil || p.Login != "" || p.Principal() != "tailscale:tag:ci" {
		t.Fatalf("a tagged device: %+v, %v", p, err)
	}
	if _, err := peerOf(&apitype.WhoIsResponse{}, self); err == nil {
		t.Fatal("an answer without a node was accepted")
	}
}
