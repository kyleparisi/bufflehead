package license

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"
)

// This file builds synthetic Mac App Store receipts so the verifier can be
// tested end to end without Apple's real root certificate, which cannot be
// checked into the repo (see anchors/README.md). The chain shape mirrors
// Apple's: a self-signed root, one intermediate, and a leaf that signs.
//
// Because the tests mint their own root and pass it in via Config.AppleRootCAs,
// they prove the verification *logic* — chain building, digest binding,
// tamper detection, device binding. Whether the shipped binary trusts the right
// root is a separate question, answered by TestAppleAnchorPresent.

type testChain struct {
	rootDER  []byte
	interDER []byte
	leafDER  []byte
	leafCert *x509.Certificate
	leafKey  *rsa.PrivateKey
}

// newTestChain builds root → intermediate → leaf. notAfter bounds every
// certificate, so passing a past time produces an expired chain.
func newTestChain(t *testing.T, notBefore, notAfter time.Time) *testChain {
	t.Helper()

	mkKey := func() *rsa.PrivateKey {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		return k
	}

	rootKey, interKey, leafKey := mkKey(), mkKey(), mkKey()

	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}

	interTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test Intermediate CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	interDER, err := x509.CreateCertificate(rand.Reader, interTmpl, rootCert, &interKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create intermediate: %v", err)
	}
	interCert, err := x509.ParseCertificate(interDER)
	if err != nil {
		t.Fatalf("parse intermediate: %v", err)
	}

	leafTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		Subject:               pkix.Name{CommonName: "Test Receipt Signing"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, interCert, &leafKey.PublicKey, interKey)
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}
	leafCert, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	return &testChain{
		rootDER: rootDER, interDER: interDER, leafDER: leafDER,
		leafCert: leafCert, leafKey: leafKey,
	}
}

// receiptParams describes the receipt payload to synthesise.
type receiptParams struct {
	BundleID        string
	AppVersion      string
	OriginalVersion string
	Opaque          []byte
	GUID            []byte
	Expiration      time.Time
	// CorruptDeviceHash writes a device hash that will not match GUID.
	CorruptDeviceHash bool
}

// buildPayload encodes the SET OF receipt attributes.
func buildPayload(t *testing.T, p receiptParams) []byte {
	t.Helper()

	bundleRaw, err := asn1.MarshalWithParams(p.BundleID, "utf8")
	if err != nil {
		t.Fatalf("marshal bundle id: %v", err)
	}
	appVerRaw, err := asn1.MarshalWithParams(p.AppVersion, "utf8")
	if err != nil {
		t.Fatalf("marshal app version: %v", err)
	}
	origVerRaw, err := asn1.MarshalWithParams(p.OriginalVersion, "utf8")
	if err != nil {
		t.Fatalf("marshal original version: %v", err)
	}

	h := sha1.New()
	h.Write(p.GUID)
	h.Write(p.Opaque)
	h.Write(bundleRaw)
	deviceHash := h.Sum(nil)
	if p.CorruptDeviceHash {
		deviceHash[0] ^= 0xFF
	}

	attrs := []receiptAttribute{
		{Type: attrBundleID, Version: 1, Value: bundleRaw},
		{Type: attrAppVersion, Version: 1, Value: appVerRaw},
		{Type: attrOpaqueValue, Version: 1, Value: p.Opaque},
		{Type: attrSHA1Hash, Version: 1, Value: deviceHash},
		{Type: attrOriginalVersion, Version: 1, Value: origVerRaw},
	}
	if !p.Expiration.IsZero() {
		expRaw, err := asn1.MarshalWithParams(p.Expiration.UTC().Format(time.RFC3339), "ia5")
		if err != nil {
			t.Fatalf("marshal expiration: %v", err)
		}
		attrs = append(attrs, receiptAttribute{Type: attrExpirationDate, Version: 1, Value: expRaw})
	}

	var body []byte
	for _, a := range attrs {
		der, err := asn1.Marshal(a)
		if err != nil {
			t.Fatalf("marshal attribute %d: %v", a.Type, err)
		}
		body = append(body, der...)
	}

	set, err := asn1.Marshal(asn1.RawValue{
		Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: body,
	})
	if err != nil {
		t.Fatalf("marshal payload set: %v", err)
	}
	return set
}

// marshalAttribute is the encoding-side shape of a PKCS#7 signed attribute.
type marshalAttribute struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue
}

// signOpts tweaks how the PKCS#7 wrapper is built, so tests can produce
// specific malformed blobs.
type signOpts struct {
	// TwoSigners duplicates the SignerInfo to exercise the single-signer rule.
	TwoSigners bool
	// OmitIntermediate leaves the intermediate out, breaking the chain.
	OmitIntermediate bool
	// TamperPayloadAfterSigning corrupts the payload after the signature is
	// computed, so the messageDigest binding is what has to catch it.
	TamperPayloadAfterSigning bool
}

// buildPKCS7 wraps payload in a signed PKCS#7 SignedData structure.
func buildPKCS7(t *testing.T, chain *testChain, payload []byte, opts signOpts) []byte {
	t.Helper()

	nullParams := asn1.RawValue{Tag: asn1.TagNull}
	digestAlg := pkix.AlgorithmIdentifier{Algorithm: oidDigestSHA256, Parameters: nullParams}
	encAlg := pkix.AlgorithmIdentifier{
		Algorithm:  asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1},
		Parameters: nullParams,
	}

	// Signed attributes: contentType and messageDigest over the payload.
	sum := crypto.SHA256.New()
	sum.Write(payload)
	digest := sum.Sum(nil)

	ctVal, err := asn1.Marshal(oidData)
	if err != nil {
		t.Fatalf("marshal contentType value: %v", err)
	}
	mdVal, err := asn1.Marshal(digest)
	if err != nil {
		t.Fatalf("marshal messageDigest value: %v", err)
	}

	var attrBody []byte
	for _, a := range []marshalAttribute{
		{Type: oidContentType, Value: asn1.RawValue{
			Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: ctVal}},
		{Type: oidMessageDigest, Value: asn1.RawValue{
			Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: mdVal}},
	} {
		der, err := asn1.Marshal(a)
		if err != nil {
			t.Fatalf("marshal signed attribute: %v", err)
		}
		attrBody = append(attrBody, der...)
	}

	// The signature covers the attributes tagged as a universal SET.
	toSign, err := asn1.Marshal(asn1.RawValue{
		Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: attrBody,
	})
	if err != nil {
		t.Fatalf("marshal signed attrs as SET: %v", err)
	}
	sigHash := crypto.SHA256.New()
	sigHash.Write(toSign)
	signature, err := rsa.SignPKCS1v15(rand.Reader, chain.leafKey, crypto.SHA256, sigHash.Sum(nil))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	si := rawSignerInfo{
		Version: 1,
		IssuerAndSerialNumber: rawIssuerAndSerial{
			Issuer:       asn1.RawValue{FullBytes: chain.leafCert.RawIssuer},
			SerialNumber: chain.leafCert.SerialNumber,
		},
		DigestAlgorithm: digestAlg,
		AuthenticatedAttributes: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: attrBody,
		},
		DigestEncryptionAlgorithm: encAlg,
		EncryptedDigest:           signature,
	}
	signers := []rawSignerInfo{si}
	if opts.TwoSigners {
		signers = append(signers, si)
	}

	certBytes := append([]byte{}, chain.leafDER...)
	if !opts.OmitIntermediate {
		certBytes = append(certBytes, chain.interDER...)
	}

	emitted := payload
	if opts.TamperPayloadAfterSigning {
		emitted = append([]byte{}, payload...)
		// Flip a byte deep inside the attribute body, leaving the outer DER
		// structure parseable so the digest check is what rejects it.
		emitted[len(emitted)-1] ^= 0x01
	}
	payloadOctets, err := asn1.Marshal(emitted)
	if err != nil {
		t.Fatalf("marshal payload octets: %v", err)
	}

	sd := rawSignedData{
		Version:          1,
		DigestAlgorithms: []pkix.AlgorithmIdentifier{digestAlg},
		ContentInfo: rawContentInfo{
			ContentType: oidData,
			Content: asn1.RawValue{
				Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: payloadOctets,
			},
		},
		Certificates: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: certBytes,
		},
		SignerInfos: signers,
	}
	sdDER, err := asn1.Marshal(sd)
	if err != nil {
		t.Fatalf("marshal SignedData: %v", err)
	}

	ci := rawContentInfo{
		ContentType: oidSignedData,
		Content: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: sdDER,
		},
	}
	ciDER, err := asn1.Marshal(ci)
	if err != nil {
		t.Fatalf("marshal ContentInfo: %v", err)
	}
	return ciDER
}
