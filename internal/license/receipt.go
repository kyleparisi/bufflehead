package license

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/asn1"
	"errors"
	"fmt"
	"time"
)

// Mac App Store receipt attribute types we care about. Apple documents these in
// the Receipt Validation Programming Guide; unlisted types are ignored so a
// future receipt format does not break parsing.
const (
	attrBundleID        = 2
	attrAppVersion      = 3
	attrOpaqueValue     = 4
	attrSHA1Hash        = 5
	attrReceiptCreated  = 12
	attrOriginalVersion = 19
	attrExpirationDate  = 21
)

// receiptAttribute mirrors the ASN.1 SEQUENCE Apple wraps each field in.
type receiptAttribute struct {
	Type    int
	Version int
	Value   []byte
}

// receipt is the decoded payload of a Mac App Store receipt.
type receipt struct {
	BundleID   string
	AppVersion string
	// OriginalVersion is the version originally purchased. It is what a paid
	// upgrade check would key on, and it survives updates.
	OriginalVersion string
	// Opaque and SHA1Hash back the device-binding check.
	Opaque   []byte
	SHA1Hash []byte
	// BundleIDRaw is the *raw DER* of the bundle identifier attribute value.
	// The device hash is computed over these bytes, not over the decoded
	// string, so it must be preserved verbatim.
	BundleIDRaw []byte

	Created    time.Time
	Expiration time.Time
}

// parseReceipt decodes the authenticated payload of a Mac App Store receipt.
// The payload is a SET OF receiptAttribute.
func parseReceipt(payload []byte) (*receipt, error) {
	var set asn1.RawValue
	rest, err := asn1.Unmarshal(payload, &set)
	if err != nil {
		return nil, fmt.Errorf("receipt: parse payload: %w", err)
	}
	if len(rest) != 0 {
		return nil, errors.New("receipt: trailing data after payload")
	}
	if set.Class != asn1.ClassUniversal || set.Tag != asn1.TagSet {
		return nil, fmt.Errorf("receipt: payload is tag %d, want SET", set.Tag)
	}

	r := &receipt{}
	body := set.Bytes
	for len(body) > 0 {
		var a receiptAttribute
		body, err = asn1.Unmarshal(body, &a)
		if err != nil {
			return nil, fmt.Errorf("receipt: parse attribute: %w", err)
		}
		switch a.Type {
		case attrBundleID:
			r.BundleIDRaw = a.Value
			if r.BundleID, err = asn1UTF8(a.Value); err != nil {
				return nil, fmt.Errorf("receipt: bundle id: %w", err)
			}
		case attrAppVersion:
			if r.AppVersion, err = asn1UTF8(a.Value); err != nil {
				return nil, fmt.Errorf("receipt: app version: %w", err)
			}
		case attrOriginalVersion:
			if r.OriginalVersion, err = asn1UTF8(a.Value); err != nil {
				return nil, fmt.Errorf("receipt: original version: %w", err)
			}
		case attrOpaqueValue:
			r.Opaque = a.Value
		case attrSHA1Hash:
			r.SHA1Hash = a.Value
		case attrReceiptCreated:
			r.Created, _ = asn1Date(a.Value)
		case attrExpirationDate:
			r.Expiration, _ = asn1Date(a.Value)
		}
	}

	if r.BundleID == "" {
		return nil, errors.New("receipt: missing bundle identifier")
	}
	if len(r.Opaque) == 0 {
		return nil, errors.New("receipt: missing opaque value")
	}
	if len(r.SHA1Hash) != sha1.Size {
		return nil, fmt.Errorf("receipt: SHA-1 field is %d bytes, want %d", len(r.SHA1Hash), sha1.Size)
	}
	return r, nil
}

// asn1UTF8 decodes a string attribute. Apple encodes these as UTF8String, but
// IA5String shows up in some fields, so accept both rather than failing a
// legitimate receipt.
func asn1UTF8(der []byte) (string, error) {
	var s string
	if _, err := asn1.UnmarshalWithParams(der, &s, "utf8"); err == nil {
		return s, nil
	}
	if _, err := asn1.UnmarshalWithParams(der, &s, "ia5"); err == nil {
		return s, nil
	}
	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(der, &raw); err != nil {
		return "", err
	}
	return string(raw.Bytes), nil
}

// asn1Date decodes an RFC 3339 date attribute. Apple encodes receipt dates as
// IA5String, and an absent date is encoded as an empty string.
func asn1Date(der []byte) (time.Time, error) {
	s, err := asn1UTF8(der)
	if err != nil || s == "" {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, s)
}

// verifyDeviceHash checks the receipt is bound to this machine.
//
// Apple's rule: SHA-1(deviceGUID ‖ opaqueValue ‖ bundleIdentifierRawDER) must
// equal attribute 5. The GUID is the primary network interface's MAC address as
// six raw bytes. This is what stops a receipt being copied from one Mac to
// another, so a mismatch is a hard rejection, not a warning.
func (r *receipt) verifyDeviceHash(guid []byte) error {
	if len(guid) == 0 {
		return errors.New("receipt: no device GUID available")
	}
	h := sha1.New()
	h.Write(guid)
	h.Write(r.Opaque)
	h.Write(r.BundleIDRaw)
	want := h.Sum(nil)
	if subtle.ConstantTimeCompare(want, r.SHA1Hash) != 1 {
		return errors.New("receipt: device hash mismatch — receipt was issued for a different Mac")
	}
	return nil
}
