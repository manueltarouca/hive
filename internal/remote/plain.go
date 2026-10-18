package remote

import "net"

func openPlain(s Spec, pol Policy) ([]*Listener, error) {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return nil, err
	}
	return []*Listener{{Listener: ln, Kind: Plain, Policy: pol}}, nil
}
