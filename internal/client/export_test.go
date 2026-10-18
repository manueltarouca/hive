package client

import (
	"crypto/x509"
	"net/http"
)

// trustForTests makes the client trust a test server's certificate.
func (c *Client) trustForTests(pool *x509.CertPool) {
	c.http.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
}
