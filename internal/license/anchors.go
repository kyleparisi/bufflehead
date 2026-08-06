package license

import (
	"crypto/x509"
	"embed"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
)

// anchorFS holds the pinned trust anchors compiled into the binary. Only
// *.cer/*.der/*.pem entries are loaded; the directory's README is ignored.
//
//go:embed anchors
var anchorFS embed.FS

// ErrNoAnchors is returned when no Apple trust anchor is compiled in. It is
// treated as inconclusive, not as a failed receipt: a build that shipped
// without its anchor is our bug, and the customer should not be locked out for
// it. See bin/fetch-apple-root.
var ErrNoAnchors = errors.New("license: no Apple trust anchor embedded (run bin/fetch-apple-root)")

var (
	anchorOnce sync.Once
	anchorPool *x509.CertPool
	anchorErr  error
)

// appleRootPool returns the pinned Apple root pool, loading it once.
func appleRootPool() (*x509.CertPool, error) {
	anchorOnce.Do(func() {
		anchorPool, anchorErr = loadAnchorsFromFS(anchorFS, "anchors")
	})
	return anchorPool, anchorErr
}

// poolFrom builds a cert pool from explicitly supplied DER or PEM blobs,
// falling back to the embedded anchors when none are given.
func poolFrom(blobs [][]byte) (*x509.CertPool, error) {
	if len(blobs) == 0 {
		return appleRootPool()
	}
	pool := x509.NewCertPool()
	n := 0
	for i, b := range blobs {
		certs, err := parseCertBlob(b)
		if err != nil {
			return nil, fmt.Errorf("license: trust anchor %d: %w", i, err)
		}
		for _, c := range certs {
			pool.AddCert(c)
			n++
		}
	}
	if n == 0 {
		return nil, ErrNoAnchors
	}
	return pool, nil
}

func loadAnchorsFromFS(fsys fs.FS, dir string) (*x509.CertPool, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, ErrNoAnchors
	}
	pool := x509.NewCertPool()
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(path.Ext(e.Name())) {
		case ".cer", ".der", ".pem", ".crt":
		default:
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("license: read anchor %s: %w", e.Name(), err)
		}
		certs, err := parseCertBlob(b)
		if err != nil {
			return nil, fmt.Errorf("license: parse anchor %s: %w", e.Name(), err)
		}
		for _, c := range certs {
			pool.AddCert(c)
			n++
		}
	}
	if n == 0 {
		return nil, ErrNoAnchors
	}
	return pool, nil
}

// parseCertBlob accepts either PEM (possibly several certificates) or raw DER.
func parseCertBlob(b []byte) ([]*x509.Certificate, error) {
	if rest := strings.TrimSpace(string(b)); strings.HasPrefix(rest, "-----BEGIN") {
		var out []*x509.Certificate
		for {
			var block *pem.Block
			block, b = pem.Decode(b)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				continue
			}
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		if len(out) == 0 {
			return nil, errors.New("no CERTIFICATE block in PEM data")
		}
		return out, nil
	}
	return x509.ParseCertificates(b)
}
