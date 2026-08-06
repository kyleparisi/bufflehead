package license

import (
	"crypto"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"

	// Registers the digests crypto.Hash.New needs at runtime. Without these
	// blank imports crypto.SHA1.New() panics instead of hashing.
	_ "crypto/sha1"
	_ "crypto/sha256"
	_ "crypto/sha512"
)

// A minimal PKCS#7 (RFC 2315) SignedData verifier, scoped to exactly what Mac
// App Store receipts use: one signer, RSA, a SHA-1/2 digest, and a certificate
// chain that must terminate at a pinned Apple root.
//
// This is hand-rolled rather than pulled from a third-party module because the
// available Go PKCS#7 packages are unmaintained, and this is the code that
// decides whether someone paid. Everything genuinely hard — chain building,
// path validation, signature primitives — is delegated to crypto/x509.
//
// Deliberate restrictions, all of which reject rather than degrade:
//   - Exactly one SignerInfo. Multi-signer receipts do not exist, and
//     "any signer verifies" is a classic PKCS#7 bypass.
//   - No CRL/OCSP. The trust anchor is pinned and offline-only by design.
//   - Digest algorithms limited to SHA-1/256/384/512; anything else is an error.

var (
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}

	oidDigestSHA1   = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidDigestSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidDigestSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidDigestSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
)

// errChainExpired marks a chain that would verify except that a certificate in
// it is outside its validity window. Callers treat this as inconclusive rather
// than as forgery: when Apple's WWDR intermediate expired in February 2023 it
// broke receipt validation industry-wide for apps that failed closed on it.
var errChainExpired = errors.New("certificate chain expired")

type rawContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type rawSignedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	ContentInfo      rawContentInfo
	Certificates     asn1.RawValue   `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue   `asn1:"optional,tag:1"`
	SignerInfos      []rawSignerInfo `asn1:"set"`
}

type rawIssuerAndSerial struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

type rawSignerInfo struct {
	Version                   int
	IssuerAndSerialNumber     rawIssuerAndSerial
	DigestAlgorithm           pkix.AlgorithmIdentifier
	AuthenticatedAttributes   asn1.RawValue `asn1:"optional,tag:0"`
	DigestEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedDigest           []byte
	UnauthenticatedAttributes asn1.RawValue `asn1:"optional,tag:1"`
}

type rawAttribute struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue `asn1:"set"`
}

// verifiedPKCS7 is the outcome of a successful verification.
type verifiedPKCS7 struct {
	// Content is the authenticated payload — for a receipt, the DER-encoded
	// attribute set.
	Content []byte
	// Signer is the leaf certificate whose signature was verified.
	Signer *x509.Certificate
}

// verifyPKCS7 parses a PKCS#7 SignedData blob, verifies the signer's chain
// against roots, and verifies the signature over the encapsulated content.
//
// verifyAt is the instant at which certificate validity is judged; pass the
// current time in production.
func verifyPKCS7(der []byte, roots *x509.CertPool, verifyAt time.Time) (*verifiedPKCS7, error) {
	if len(der) == 0 {
		return nil, errors.New("pkcs7: empty input")
	}
	if roots == nil {
		return nil, errors.New("pkcs7: no trust anchors configured")
	}

	var ci rawContentInfo
	rest, err := asn1.Unmarshal(der, &ci)
	if err != nil {
		return nil, fmt.Errorf("pkcs7: parse ContentInfo: %w", err)
	}
	if len(rest) != 0 {
		return nil, errors.New("pkcs7: trailing data after ContentInfo")
	}
	if !ci.ContentType.Equal(oidSignedData) {
		return nil, fmt.Errorf("pkcs7: content type %v is not signedData", ci.ContentType)
	}

	var sd rawSignedData
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("pkcs7: parse SignedData: %w", err)
	}

	// Exactly one signer. Accepting the first of several that verifies is a
	// well-known way to smuggle an attacker-controlled signature past a check.
	if len(sd.SignerInfos) != 1 {
		return nil, fmt.Errorf("pkcs7: expected exactly 1 signer, got %d", len(sd.SignerInfos))
	}
	si := sd.SignerInfos[0]

	if !sd.ContentInfo.ContentType.Equal(oidData) {
		return nil, fmt.Errorf("pkcs7: encapsulated type %v is not data", sd.ContentInfo.ContentType)
	}
	// The payload is an OCTET STRING inside the [0] EXPLICIT wrapper.
	var payload []byte
	if _, err := asn1.Unmarshal(sd.ContentInfo.Content.Bytes, &payload); err != nil {
		return nil, fmt.Errorf("pkcs7: parse encapsulated content: %w", err)
	}
	if len(payload) == 0 {
		return nil, errors.New("pkcs7: encapsulated content is empty")
	}

	certs, err := x509.ParseCertificates(sd.Certificates.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pkcs7: parse certificates: %w", err)
	}
	if len(certs) == 0 {
		return nil, errors.New("pkcs7: no certificates in SignedData")
	}

	signer := findSigner(certs, si.IssuerAndSerialNumber)
	if signer == nil {
		return nil, errors.New("pkcs7: signer certificate not found in SignedData")
	}

	// Chain to a pinned root. Every non-leaf certificate in the blob is offered
	// as a candidate intermediate; x509 decides which actually chain.
	intermediates := x509.NewCertPool()
	for _, c := range certs {
		if !c.Equal(signer) {
			intermediates.AddCert(c)
		}
	}
	_, err = signer.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   verifyAt,
		// Apple's receipt-signing leaf carries no serverAuth/clientAuth EKU;
		// pinning the root is what constrains this chain, not EKU filtering.
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		var invalid x509.CertificateInvalidError
		if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
			return nil, fmt.Errorf("%w: %v", errChainExpired, err)
		}
		return nil, fmt.Errorf("pkcs7: chain does not reach a pinned root: %w", err)
	}

	hashFn, err := digestForOID(si.DigestAlgorithm.Algorithm)
	if err != nil {
		return nil, err
	}

	// Determine what was actually signed. With authenticated attributes
	// present, the signature covers the DER SET OF attributes, and the
	// messageDigest attribute is what binds that signature to the payload —
	// so it must be checked, or the signature says nothing about the content.
	signed := payload
	if len(si.AuthenticatedAttributes.Bytes) > 0 {
		attrs, err := parseAttributes(si.AuthenticatedAttributes.Bytes)
		if err != nil {
			return nil, err
		}
		if err := checkMessageDigest(attrs, payload, hashFn); err != nil {
			return nil, err
		}
		if err := checkAttrContentType(attrs); err != nil {
			return nil, err
		}
		// Re-tag the [0] IMPLICIT wrapper back to a universal SET for signing,
		// per RFC 2315 §9.3.
		signed, err = reTagAsSet(si.AuthenticatedAttributes)
		if err != nil {
			return nil, err
		}
	}

	if err := rsaSanity(signer); err != nil {
		return nil, err
	}
	sigAlg, err := signatureAlgorithm(si.DigestEncryptionAlgorithm.Algorithm, hashFn)
	if err != nil {
		return nil, err
	}
	if err := signer.CheckSignature(sigAlg, signed, si.EncryptedDigest); err != nil {
		return nil, fmt.Errorf("pkcs7: signature verification failed: %w", err)
	}

	return &verifiedPKCS7{Content: payload, Signer: signer}, nil
}

// findSigner locates the certificate matching a SignerInfo's issuer and serial.
func findSigner(certs []*x509.Certificate, ias rawIssuerAndSerial) *x509.Certificate {
	for _, c := range certs {
		if c.SerialNumber == nil || ias.SerialNumber == nil {
			continue
		}
		if c.SerialNumber.Cmp(ias.SerialNumber) != 0 {
			continue
		}
		// RawIssuer is the DER of the issuer Name; compare bytes so we do not
		// depend on string canonicalisation of the RDN sequence.
		if string(c.RawIssuer) == string(ias.Issuer.FullBytes) {
			return c
		}
	}
	return nil
}

// parseAttributes decodes the concatenated attributes inside an implicitly
// tagged [0] wrapper.
func parseAttributes(der []byte) ([]rawAttribute, error) {
	var out []rawAttribute
	for len(der) > 0 {
		var a rawAttribute
		rest, err := asn1.Unmarshal(der, &a)
		if err != nil {
			return nil, fmt.Errorf("pkcs7: parse authenticated attribute: %w", err)
		}
		out = append(out, a)
		der = rest
	}
	if len(out) == 0 {
		return nil, errors.New("pkcs7: empty authenticated attributes")
	}
	return out, nil
}

// checkMessageDigest verifies the messageDigest attribute equals the digest of
// the encapsulated content. Without this the signature is unbound from the
// payload and an attacker could staple a valid signature to any receipt.
func checkMessageDigest(attrs []rawAttribute, payload []byte, h crypto.Hash) error {
	for _, a := range attrs {
		if !a.Type.Equal(oidMessageDigest) {
			continue
		}
		var got []byte
		if _, err := asn1.Unmarshal(a.Value.Bytes, &got); err != nil {
			return fmt.Errorf("pkcs7: parse messageDigest: %w", err)
		}
		hasher := h.New()
		hasher.Write(payload)
		want := hasher.Sum(nil)
		if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			return errors.New("pkcs7: messageDigest does not match content")
		}
		return nil
	}
	return errors.New("pkcs7: authenticated attributes present but messageDigest is missing")
}

// checkAttrContentType verifies the signed contentType attribute agrees with
// the encapsulated content type, per RFC 2315 §9.2.
func checkAttrContentType(attrs []rawAttribute) error {
	for _, a := range attrs {
		if !a.Type.Equal(oidContentType) {
			continue
		}
		var got asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(a.Value.Bytes, &got); err != nil {
			return fmt.Errorf("pkcs7: parse contentType attribute: %w", err)
		}
		if !got.Equal(oidData) {
			return fmt.Errorf("pkcs7: signed contentType %v is not data", got)
		}
		return nil
	}
	return errors.New("pkcs7: authenticated attributes present but contentType is missing")
}

// reTagAsSet rebuilds an implicitly tagged [0] attribute block as a universal
// SET OF, which is the byte sequence the signature actually covers.
func reTagAsSet(rv asn1.RawValue) ([]byte, error) {
	set := asn1.RawValue{
		Class:      asn1.ClassUniversal,
		Tag:        asn1.TagSet,
		IsCompound: true,
		Bytes:      rv.Bytes,
	}
	out, err := asn1.Marshal(set)
	if err != nil {
		return nil, fmt.Errorf("pkcs7: re-encode authenticated attributes: %w", err)
	}
	return out, nil
}

func digestForOID(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(oidDigestSHA1):
		return crypto.SHA1, nil
	case oid.Equal(oidDigestSHA256):
		return crypto.SHA256, nil
	case oid.Equal(oidDigestSHA384):
		return crypto.SHA384, nil
	case oid.Equal(oidDigestSHA512):
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("pkcs7: unsupported digest algorithm %v", oid)
	}
}

// signatureAlgorithm maps a digest-encryption OID plus digest to an x509
// SignatureAlgorithm. Apple receipts are RSA PKCS#1 v1.5; the rsaEncryption OID
// is the common encoding, but some producers repeat the combined
// sha*WithRSAEncryption OID here instead.
func signatureAlgorithm(encOID asn1.ObjectIdentifier, h crypto.Hash) (x509.SignatureAlgorithm, error) {
	oidRSAEncryption := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	combined := map[string]x509.SignatureAlgorithm{
		"1.2.840.113549.1.1.5":  x509.SHA1WithRSA,
		"1.2.840.113549.1.1.11": x509.SHA256WithRSA,
		"1.2.840.113549.1.1.12": x509.SHA384WithRSA,
		"1.2.840.113549.1.1.13": x509.SHA512WithRSA,
	}
	if alg, ok := combined[encOID.String()]; ok {
		return alg, nil
	}
	if !encOID.Equal(oidRSAEncryption) {
		return 0, fmt.Errorf("pkcs7: unsupported signature algorithm %v", encOID)
	}
	switch h {
	case crypto.SHA1:
		return x509.SHA1WithRSA, nil
	case crypto.SHA256:
		return x509.SHA256WithRSA, nil
	case crypto.SHA384:
		return x509.SHA384WithRSA, nil
	case crypto.SHA512:
		return x509.SHA512WithRSA, nil
	default:
		return 0, fmt.Errorf("pkcs7: unsupported digest %v for RSA", h)
	}
}

// rsaSanity keeps the rsa import meaningful: receipts must be RSA-signed, and a
// non-RSA signer key is rejected before CheckSignature is reached.
func rsaSanity(c *x509.Certificate) error {
	if _, ok := c.PublicKey.(*rsa.PublicKey); !ok {
		return fmt.Errorf("pkcs7: signer key is %T, want RSA", c.PublicKey)
	}
	return nil
}
