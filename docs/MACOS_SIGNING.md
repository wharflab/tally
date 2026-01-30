# macOS Binary Signing Setup

This document guides you through setting up macOS binary signing and notarization for tally releases.

## Why Sign macOS Binaries?

Unsigned binaries on macOS trigger Gatekeeper warnings that prevent users from running them. Signing and notarizing binaries provides:

- ✅ No Gatekeeper warnings for end users
- ✅ Verification that binaries haven't been tampered with
- ✅ Professional distribution experience
- ✅ Required for distribution on modern macOS versions

## Prerequisites

### 1. Apple Developer Account

- **Required**: Paid Apple Developer Program membership ($99/year)
- Sign up at: <https://developer.apple.com/programs/>
- Log in to: <https://developer.apple.com/account>

### 2. Create Developer ID Application Certificate

This certificate is specifically for distribution **outside** the Mac App Store.

1. Go to: <https://developer.apple.com/account>
2. Navigate to: **Certificates, Identifiers & Profiles**
3. Click **"+"** to create a new certificate
4. Select **"Developer ID Application"**
5. Follow instructions to create a Certificate Signing Request (CSR):
   - Open **Keychain Access** on your Mac
   - Menu: **Keychain Access → Certificate Assistant → Request a Certificate from a Certificate Authority**
   - Enter your email and name
   - Select **"Saved to disk"** and click Continue
   - Save the `.certSigningRequest` file
6. Upload the CSR file to the Apple Developer portal
7. Download the certificate (`.cer` file)
8. Double-click the `.cer` file to install it in your Keychain

### 3. Export Certificate for GitHub Actions

The certificate needs to be converted to a format that GitHub Actions can use.

1. Open **Keychain Access**
2. Make sure you're in the **"login"** keychain (top left)
3. Find **"Developer ID Application: Your Name"** in the "My Certificates" category
4. **Important**: Click the arrow (▸) to expand the certificate
5. You should see a **private key** with a key icon 🔑 underneath
6. **Select the private key** (not the certificate itself)
7. **Right-click** → **Export**
8. **File format**: Personal Information Exchange (.p12) - should now be available!
9. Choose a **strong password** (you'll need this for GitHub secrets)
10. Save the file as `certificate.p12`

**Troubleshooting**: If .p12 is grayed out:

- The private key is missing or not selected
- Make sure you expanded the certificate to see the private key
- Or select the certificate entry that shows both cert + key icons
- If no private key exists, you need to recreate the CSR (see step 2)

11. Convert the certificate to base64:

   ```bash
   base64 -i certificate.p12 | pbcopy
   ```

   This copies the base64-encoded certificate to your clipboard.

### 4. Create App-Specific Password for Notarization

Notarization requires an app-specific password (not your Apple ID password).

1. Go to: <https://appleid.apple.com/account/manage>
2. Sign in with your Apple ID
3. Navigate to: **Security → App-Specific Passwords**
4. Click **"Generate Password..."**
5. Name it: `tally-notarization`
6. **Copy the generated password** (format: `xxxx-xxxx-xxxx-xxxx`)
7. Save it securely - you cannot view it again

### 5. Find Your Team ID

1. Go to: <https://developer.apple.com/account>
2. Look in the **upper right corner** - your Team ID is displayed there
3. It's a 10-character alphanumeric string (e.g., `AB12CD34EF`)

## GitHub Secrets Configuration

Add these secrets to your GitHub repository:

Go to: <https://github.com/tinovyatkin/tally/settings/secrets/actions>

| Secret Name | Value | Description |
|------------|-------|-------------|
| `APPLE_CERTIFICATE_BASE64` | Base64-encoded .p12 file | From step 3 above |
| `APPLE_CERTIFICATE_PASSWORD` | Password you set | The password used when exporting the .p12 |
| `APPLE_ID` | Your Apple ID email | The email for your Apple Developer account |
| `APPLE_APP_PASSWORD` | App-specific password | From step 4 above (format: xxxx-xxxx-xxxx-xxxx) |
| `APPLE_TEAM_ID` | Your Team ID | From step 5 above (10-character string) |

## GitHub Actions Integration

The signing workflow has been prepared in the plan but **not yet implemented** in `.github/workflows/release.yml`.

To implement:

1. Review the signing steps in the implementation plan
2. Add the signing steps to your release workflow after the build step
3. Test with a pre-release tag: `git tag v0.1.1-test && git push origin v0.1.1-test`

### What the Workflow Does

1. **Import Certificate**: Creates a temporary keychain and imports the signing certificate
2. **Sign Binaries**: Signs all darwin binaries with the Developer ID
3. **Notarize**: Submits binaries to Apple's notarization service
4. **Staple**: Attaches the notarization ticket to the binary

## Testing the Setup

After adding the secrets and implementing the workflow:

1. Create a test release:

   ```bash
   git tag v0.1.1-test
   git push origin v0.1.1-test
   ```

2. Wait for the GitHub Action to complete

3. Download the macOS binary from the release

4. Verify the signature:

   ```bash
   codesign --verify --verbose tally-darwin-amd64
   ```

   Expected output:

   ```text
   tally-darwin-amd64: valid on disk
   tally-darwin-amd64: satisfies its Designated Requirement
   ```

5. Run the binary - it should **not** show any Gatekeeper warnings

## Troubleshooting

### "no identity found"

- The certificate wasn't imported correctly into the keychain
- Check that the base64 encoding is correct
- Verify the password matches what you set

### Notarization timeout

- Apple's notarization service can take 5-30 minutes
- The workflow uses `--wait` to handle this automatically
- Check Apple Developer portal for rejection reasons if it fails

### "invalid signature"

- The binary was modified after signing
- Rebuild and sign again
- Make sure no post-processing steps modify the binary

### Gatekeeper still blocking

- Notarization failed - check the notarization logs
- Go to: <https://developer.apple.com/account>
- Check **Notary > History** for rejection details
- Common issues:
  - Hardened runtime not enabled (`--options runtime` flag required)
  - Binary architecture mismatch
  - Entitlements issues

## Security Notes

- **Never commit** the .p12 file or passwords to git
- The temporary keychain in the workflow is **deleted** after use
- Secrets are only accessible during the workflow run
- The signing happens on GitHub-hosted runners (trusted environment)

## Cost

- Apple Developer Program: **$99/year** (required)
- GitHub Actions: **Free** for public repositories
- Notarization: **Free** (included with Apple Developer membership)

## References

- [Apple Code Signing Guide](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/Introduction/Introduction.html)
- [Notarizing macOS Software](https://developer.apple.com/documentation/security/notarizing_macos_software_before_distribution)
- [xcrun notarytool](https://developer.apple.com/documentation/security/notarizing_macos_software_before_distribution/customizing_the_notarization_workflow)
