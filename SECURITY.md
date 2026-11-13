# Security policy

SecureSpeak is end-to-end encrypted voice and text chat, so security problems
matter a lot here. Thank you for helping to find and fix them.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report them privately through GitHub instead:

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability**.
3. Describe the problem, the affected version and, if possible, steps to reproduce it.

Only the maintainers can see the report. We'll work with you on a fix in a
private advisory and credit you when the fix is published, unless you'd rather
stay anonymous.

SecureSpeak is a small project maintained in spare time. We aim to respond to
reports within **7 days** and to fix serious issues as quickly as possible.
There is no bug bounty.

## Supported versions

Only the **latest release** receives security fixes. Please check that a
problem still exists in the latest version before reporting it.

## What is in scope

Anything that breaks the promises in the README's *Security design* section,
for example:

- Reading, altering or forging voice, video, screen share sound, text, pokes,
  avatars or files without being an intended recipient. This includes a
  malicious **server**: it must never be able to decrypt or inject content.
- Impersonating someone's identity, or obtaining roles, owner status or
  permissions you shouldn't have (for example bypassing the power hierarchy or
  the owner protection).
- Reusing, guessing or bypassing privilege keys or the server password.
- Problems in the local app: another program or website on the same computer
  talking to the client's local interface, running code through a received
  file, preview or chat message, or writing files outside the download folder.
- Crashes or resource exhaustion of the **server** that an ordinary,
  unprivileged user can trigger remotely.
- Problems in the Windows installer or the Linux packages.
- Making the built-in updater install something other than a genuine release,
  for example by getting it past the SHA-256 checks.

## Known limitations (not vulnerabilities)

These are documented trade-offs. Ideas to improve them are welcome as normal
issues:

- The server sees **metadata**: IP addresses, nicknames, public keys, channel
  membership, who talks, whispers or shares a screen and when, and message,
  file and frame sizes.
- Identity is trust-on-first-use. Without comparing fingerprints, a malicious
  server could substitute keys when you first meet someone. Verifying
  fingerprints prevents this.
- Release binaries are not code-signed yet, so Windows SmartScreen warns about
  them.
- Server admins can moderate users with less power than they have. That is
  intended.
- The cryptography has been tested but not independently audited.
- Release signing: when public keys are listed in
  `internal/update/signing-keys.pem`, the updater only installs releases whose
  `SHA256SUMS` carries a valid Ed25519 signature (`SHA256SUMS.sig`) by one of
  those keys, so a GitHub account takeover alone can't push an update. The
  private key is a GitHub Actions secret, so someone who can change the
  workflows or secrets of this repository could still sign. Copies installed
  before signing was switched on only check SHA-256 checksums.
