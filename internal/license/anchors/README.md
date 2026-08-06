# Pinned trust anchors

Certificates in this directory are compiled into the binary with `go:embed` and
used as the **only** trust anchors for Mac App Store receipt verification. The
system trust store is deliberately not consulted: a receipt must chain to Apple,
and nothing else.

Files ending in `.cer`, `.der`, `.pem` or `.crt` are loaded; everything else
(including this README) is ignored. Both DER and PEM encodings work.

## What belongs here

`AppleIncRootCertificate.cer` — the **Apple Inc. Root** certificate, the root of
the chain Apple signs App Store receipts under.

This file is **not checked in yet**, because the sandbox this was scaffolded in
cannot reach `apple.com` and a trust anchor must never be transcribed from
memory or pulled from an unverified mirror. Until it is added, receipt
validation returns `ErrNoAnchors`, which the resolver treats as *inconclusive*
and fails open into the grace window — it does not reject anyone's receipt.

## Adding it

```bash
./bin/fetch-apple-root
```

The script downloads the certificate, prints its SHA-256 fingerprint, subject
and validity window, and asks you to compare the fingerprint against the value
Apple publishes at <https://www.apple.com/certificateauthority/> before the file
is kept. Confirm it by eye — that comparison is the entire security value of
pinning, and no script should do it for you.

Once the file is in place, `go test ./internal/license/` picks it up
automatically: `TestAppleAnchorPresent` stops skipping and asserts the anchor
parses, is self-signed, and is currently within its validity window.
