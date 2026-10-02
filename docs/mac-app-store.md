# Shipping to the Mac App Store

The store build is a separate channel from the Developer ID DMG. It's built
with the `mas` Go tag and a locked DuckDB engine that can't load extensions,
it runs sandboxed, and it's signed with different certificates. The
engineering background and validation record are in
`packaging/macos/mas/README.md`. This page covers getting it signed and into
App Store Connect.

| | Developer ID (DMG) | Mac App Store (pkg) |
|---|---|---|
| Script | `bin/sign-notarize` | `bin/sign-mas` |
| Workflow | `build-macos.yml` | `build-mas.yml` |
| App cert | Developer ID Application | **Apple Distribution** |
| Package cert | — | **Mac Installer Distribution** (shows in Keychain as "3rd Party Mac Developer Installer") |
| Profile | none | **Mac App Store Connect** provisioning profile |
| Entitlements | `packaging/macos/entitlements.plist` | `packaging/macos/mas/entitlements.plist` (+ app/team ID from the profile) |
| Apple check | notarization | App Store Connect upload + App Review |

## One-time setup (≈30 minutes, all in a browser + Keychain Access)

Team ID below is `63GMD6U4J2`; the bundle ID is `com.kyleparisi.bufflehead`
(from `graphics/export_presets.cfg`, preset "macOS").

### 1. Register the App ID

developer.apple.com → Certificates, IDs & Profiles → **Identifiers** → `+` →
App IDs → App → **Explicit** bundle ID `com.kyleparisi.bufflehead`,
description "Bufflehead". No extra capabilities are needed; App Sandbox is an
entitlement, not a portal capability. If the ID already exists, for example from
the Developer ID setup, reuse it.

### 2. Create the two certificates

You need one Certificate Signing Request (CSR) for both. On your Mac:
Keychain Access → menu **Keychain Access → Certificate Assistant → Request a
Certificate From a Certificate Authority…** → your email, name "Kyle Parisi",
**Saved to disk** → `CertificateSigningRequest.certSigningRequest`.

Then in **Certificates** → `+`, twice:

1. **Apple Distribution** → upload the CSR → download `distribution.cer`.
2. **Mac Installer Distribution** → upload the same CSR → download
   `mac_installer_distribution.cer`.

Double-click both `.cer` files so they install into the **login** keychain,
next to the private key the CSR created. Check:

```bash
security find-identity -v -p codesigning | grep "Apple Distribution"
security find-identity -v | grep "3rd Party Mac Developer Installer"
```

Both must show up. If one is missing, the `.cer` went into a different
keychain than the private key. Drag it into "login".

> An account only gets a few Distribution certs. If the portal refuses to make
> one, revoke an unused old one, or reuse an existing Apple Distribution cert
> whose private key is still on this Mac.

### 3. Create the provisioning profile

**Profiles** → `+` → Distribution → **Mac App Store Connect** → App ID
`com.kyleparisi.bufflehead` → certificate: the Apple Distribution one →
name it `Bufflehead Mac App Store` → download
`Bufflehead_Mac_App_Store.provisionprofile`.

Pick "Mac App Store Connect", not "Developer ID" or "macOS App Development".
`bin/sign-mas` checks the profile type and bundle ID and refuses a wrong one
before building.

### 4. Create the app record in App Store Connect

appstoreconnect.apple.com → Apps → `+` → **New App** → platform macOS, name
"Bufflehead" (it must be unique on the store; have a fallback ready, such as
"Bufflehead – Parquet Viewer"), primary language, bundle ID
`com.kyleparisi.bufflehead`, SKU `bufflehead`.

### 5. Check the API key's role

The workflow reuses the Developer ID notarization key (`APPLE_API_KEY_*`).
Notarization works with the **Developer** role, but **uploading a build needs
App Manager or Admin**. In App Store Connect → Users and Access →
Integrations → App Store Connect API, check the key's role. If it's
Developer, create a new App Manager key and use it for both workflows.

### 6. Put the credentials in GitHub

Export both certs **into one `.p12`**: in Keychain Access → login → My
Certificates, ⌘-click "Apple Distribution: Kyle Parisi" **and** "3rd Party Mac
Developer Installer: Kyle Parisi" → right-click → **Export 2 items…** →
`.p12`, set a password.

```bash
base64 -i Certificates.p12 | gh secret set MAS_CERT_P12_BASE64
gh secret set MAS_CERT_PASSWORD            # paste the .p12 password
base64 -i Bufflehead_Mac_App_Store.provisionprofile | gh secret set MAS_PROFILE_BASE64
# APPLE_API_KEY_P8_BASE64 / _ID / _ISSUER_ID already exist for build-macos.yml
```

Then delete the exported `.p12` from disk.

## Building and uploading

### Via CI (preferred)

```bash
gh workflow run build-mas.yml -f upload=true    # or upload=false for a dry run
gh run watch
```

The first run builds DuckDB from source for both architectures, which takes
about an hour. The workflow caches the result, so later runs take about 20
minutes. `BUILD_NUMBER` is the workflow run number, so every upload gets a
fresh `CFBundleVersion` automatically. The signed `.pkg` is also kept as the
`Bufflehead-mas-pkg` artifact.

### Locally

```bash
# Once per engine change (needs Xcode, cmake, ninja; a DuckDB checkout at
# 19864453f7d0ed095256d848b46e7b8630989bac):
./bin/build-duckdb-mas --source ~/src/duckdb --output ~/build/duckdb-arm64  --arch arm64  --jobs 8
./bin/build-duckdb-mas --source ~/src/duckdb --output ~/build/duckdb-x86_64 --arch x86_64 --jobs 8

MAS_APP_IDENTITY="Apple Distribution: Kyle Parisi (63GMD6U4J2)" \
MAS_INSTALLER_IDENTITY="3rd Party Mac Developer Installer: Kyle Parisi (63GMD6U4J2)" \
MAS_PROFILE=~/Downloads/Bufflehead_Mac_App_Store.provisionprofile \
DUCKDB_AMD64=~/build/duckdb-x86_64 DUCKDB_ARM64=~/build/duckdb-arm64 \
BUILD_NUMBER=1 \
  ./bin/sign-mas
```

Then drag `releases/Bufflehead-mas.pkg` into Apple's **Transporter** app, or
rerun with `UPLOAD=1` and the `APPLE_API_KEY_*` variables set.

### What `bin/sign-mas` does

1. `gd build` exports the universal `.app`.
2. `bin/prepare-mas-bundle --arch universal` builds the Go library with
   `-tags mas` against each locked engine, merges the two slices with `lipo`,
   and bundles the license notices and engine provenance.
3. It sets `CFBundleVersion`, `LSMinimumSystemVersion` (11.0, because the
   engine is built for 11.0) and `ITSAppUsesNonExemptEncryption=false` in the
   Info.plist.
4. It embeds the provisioning profile and signs nested dylibs (no
   entitlements), then the app with the sandbox entitlements plus
   `application-identifier`/`team-identifier` from the profile. It refuses to
   continue if `disable-library-validation` ends up in the signature.
5. `productbuild` creates the `.pkg`, signed with the installer identity.
6. With `UPLOAD=1` it runs `altool --validate-app` and then `--upload-app`.

## Submitting for review (in App Store Connect)

After the build finishes processing (10–30 minutes, and you get an email),
fill in the app page:

- **Screenshots**: at least one at 1280×800, 1440×900, 2560×1600 or 2880×1800.
- **Description, keywords, support URL, privacy policy URL.** A privacy
  policy is required even when no data is collected. A short page saying
  "Bufflehead does not collect data; files stay on your Mac" is fine.
- **App Privacy**: "Data Not Collected", unless the store build sends
  anything off the machine.
- **Category**: Developer Tools (already set in the bundle).
- **Pricing**, then select the processed build and **Submit for Review**.

**Notes for the reviewer.** Use the "App Review Information → Notes" field to
explain these points ahead of time, because they draw questions from
reviewers:

- `network.server`: Bufflehead runs a **localhost-only** HTTP control API,
  protected by a per-launch bearer key, for automation and its "copy AI prompt"
  feature. It doesn't accept outside connections.
- `network.client`: it connects to the user's own Postgres, MySQL and SSH
  hosts.
- DuckDB extension loading is compiled out, and the self-updater is excluded
  from the store build (`mas` tag).
- Files are opened through the system Open panel or drag-and-drop
  (security-scoped bookmarks). Give a sample Parquet file and the steps to
  open it.

## Before the first real submission

From `packaging/macos/mas/README.md`, these are still open:

- Rerun the functional checks against the **store-signed** build. The
  recorded validation used ad-hoc signing. A TestFlight build is the easiest
  way to install the real signed app.
- Cloud workflows that need `httpfs` (S3, GCS) aren't available in the locked
  engine. Leave them out of the store description.
- SSH-agent setup inside the sandbox isn't a finished user-facing flow yet.
- The folder-access UI doesn't have revoke/regrant yet.
