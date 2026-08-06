# Licensing

Bufflehead ships through several channels from one binary. `internal/license`
decides, at runtime, which channel a running copy came from and what it is
entitled to.

## Detection order

`NewResolver` builds a fixed chain. The first provider whose channel signal is
present claims the process, and its verdict is final.

| # | Provider | Channel | Signal | Status |
|---|----------|---------|--------|--------|
| 0 | `InternalProvider` | `internal` | Allowlisted internal key (or company email) | **Implemented** |
| 1 | `StoreKitProvider` | `mas` | `Contents/_MASReceipt/receipt` in the bundle | **Implemented** |
| 2 | `SetappProvider` | `setapp` | Setapp SDK runtime check | Scaffolded |
| 3 | `OfflineFileProvider` | `mdm` | Signed license file dropped by MDM | Scaffolded |
| 4 | `RemoteAPIProvider` | `direct` | User-entered key + cached token | Scaffolded |

### Detection and validation are separate calls

`Detect` answers *"are we running under this channel?"*. `Validate` answers
*"is this copy entitled?"*. Collapsing them looks harmless and is not: if a torn
Mac App Store receipt made the chain fall through, a legitimately broken MAS
install would be re-examined as an MDM or direct-sale install, and the "stop at
the first match" rule would quietly become "try every channel until one says
yes". So a detected channel owns the outcome — a bad receipt yields an
unlicensed MAS status, never a fallthrough.

`InternalProvider` is the documented exception. There is no structural signal
for "this person works here", so possession of a valid internal key *is* the
channel signal. That is safe in the direction that matters: an invalid key means
"not the internal channel", and the chain moves on to the real one.

### The scaffolded providers deliberately never detect

`SetappProvider`, `OfflineFileProvider` and `RemoteAPIProvider` all return
`Detect() == false`. That is load-bearing, not laziness. A provider that
detected but could not validate would hit the fail-open path and be granted a
grace window — for `OfflineFileProvider` that is a live privilege escalation,
since anyone who can `touch /Library/Application Support/bufflehead/license.plist`
would get two weeks of free access. Each provider starts detecting in the same
change that lands its verification.

## Fail-open policy

Locking out a paying customer is worse than a few days of unlicensed use, so
`Resolve` never returns an error and never hard-blocks:

| Situation | Result |
|-----------|--------|
| No channel detected | Unlicensed → nag screen |
| Channel detected, conclusive verdict | That verdict stands, licensed or not |
| Channel detected, **inconclusive** (network down, unreadable file, missing trust anchor) | Grace window (default 14 days), full access |
| Grace window elapsed | Unlicensed → nag screen |

A *forged* license is a conclusive negative and gets no grace window. Only
genuine uncertainty fails open. Expiry is enforced centrally in `Resolve` so an
individual provider cannot forget to.

## Internal / coworker access

Internal access runs on **keys**, not email domains.

The original plan preferred email-domain matching "if we already have some
login/identity step". Bufflehead does not have one. The only sign-in in the app
is AWS SSO, which authenticates the user to *their own* AWS account against an
IdP *they* configure — anyone can point it at an IdP asserting any address they
like, so it cannot carry an entitlement. `Config.Identity` is the seam for a real
Bufflehead account system later; until something sets it, the email path is
inert and fully tested as such.

Keys are stored as **SHA-256 digests**, never as plaintext. Shipping the keys
themselves would put working coworker credentials in every customer's copy,
recoverable with `strings`. Digests are safe here because internal keys are
high-entropy random strings.

Issue a key:

```bash
KEY="BUFF-INTERNAL-$(openssl rand -hex 12 | tr 'a-f' 'A-F')"
echo "key (give to coworker): $KEY"
echo "hash (add to build):    $(printf %s "$KEY" | shasum -a 256 | cut -d' ' -f1)"
```

Build with the allowlist compiled in:

```bash
go build -ldflags "-X bufflehead/internal/license.internalKeyHashes=<hash1>,<hash2>" ./cmd/viewer
```

A coworker supplies it via `BUFFLEHEAD_INTERNAL_KEY`, or saves it once with
`license.StoreInternalKey` (OS keychain).

## Mac App Store

### Receipt file, not the StoreKit 2 API

"StoreKit 2 receipt validation" names two different mechanisms:

- **`Contents/_MASReceipt/receipt`** — a PKCS#7 blob the App Store writes at
  install time. Verifying it is pure computation over bytes on disk: no Apple
  frameworks, no cgo, works from Go.
- **StoreKit 2 `AppTransaction`/`Transaction`** — JWS objects from async Swift
  APIs on macOS 12+. Reaching them from this codebase needs a cgo bridge into a
  Swift/Objective-C shim linked into the Godot host.

Both are authoritative. For a paid-up-front app the receipt file answers the
entitlement question completely, so `StoreKitProvider` uses it and stays
cgo-free. StoreKit 2 becomes necessary only if in-app purchases or subscriptions
are added, since those transactions are not in the app receipt; `Validate` is
the seam.

### What is verified

1. PKCS#7 signature, with **exactly one** signer (accepting "any signer that
   verifies" is a classic bypass).
2. Certificate chain to a **pinned Apple root** — the system trust store is
   deliberately not consulted.
3. `messageDigest` signed attribute matches the payload, so the signature is
   actually bound to the receipt contents.
4. Bundle ID and version match this build.
5. **Device hash**: `SHA-1(primaryMAC ‖ opaqueValue ‖ bundleIdDER)` equals the
   receipt's field 5. This is what stops a receipt being copied between Macs.

An expired certificate chain is treated as *inconclusive*, not as forgery. When
Apple's WWDR intermediate expired in February 2023 it broke receipt validation
industry-wide for apps that failed closed on it.

### The trust anchor is not in the repo

`internal/license/anchors/` is empty of certificates. Run:

```bash
./bin/fetch-apple-root
```

It downloads the Apple Inc. Root certificate, checks it is a parseable,
self-signed DER cert, prints its SHA-256 fingerprint, and asks you to compare it
by eye against <https://www.apple.com/certificateauthority/>. That comparison is
the entire security value of pinning, so no script does it for you.

Until the anchor is present, receipt validation returns `ErrNoAnchors`, which
fails open into the grace window — it never rejects a customer's receipt.

### Exit code 173

A Mac App Store build with a missing or invalid receipt **must not** show a nag
screen. It must `exit(173)`, which is macOS's signal to prompt for an Apple ID,
write a fresh receipt into the bundle, and relaunch. Nagging instead leaves the
user with no way to ever obtain a receipt, and fails App Review — the reviewer's
first launch *is* the no-receipt case.

`license.Gate` handles this. It is deliberately narrow: real macOS, MAS channel,
receipt was the problem. A developer running `gd run` is never killed by it.

```go
gate := license.NewGate(license.Config{BundleVersion: version})
status := gate.Resolve(ctx) // may exit(173) and not return
if status.ShouldNag() {
    // show nag UI; status.Reason is user-facing
}
```

## Blockers before the Mac App Store path can ship

These are distribution problems, not code problems, and both need a decision:

1. **App Sandbox is off.** `graphics/export_presets.cfg` sets
   `codesign/entitlements/app_sandbox/enabled=false`. The Mac App Store requires
   the sandbox. Turning it on affects the file-open flow, the AWS SSM tunnel
   subprocess, and the local control server.

2. **DuckDB downloads extensions at runtime.** That is why the Developer ID
   build needs `disable-library-validation`. Downloading and loading executable
   code is a hard App Store rejection (guideline 2.5.2). A MAS build likely has
   to statically link or pre-bundle every DuckDB extension it needs and disable
   remote extension installation.

The licensing code is ready for the channel either way — but it is worth
resolving these before investing in the MAS submission, since they may change
whether that channel is viable at all.

## Testing

```bash
go test ./internal/license/
```

The suite mints its own certificate chain and synthesises receipts, so the
verifier is tested end to end without Apple's root: a genuine receipt validates,
and nine distinct forgeries are rejected (tampered payload, untrusted root,
missing intermediate, two signers, wrong app, wrong version, wrong Mac, corrupt
device hash, non-PKCS#7 data). `TestAppleAnchorPresent` skips until you run
`bin/fetch-apple-root`.
