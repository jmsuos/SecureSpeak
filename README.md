<p align="center"><img src="assets/securespeak-logo.png" alt="SecureSpeak" width="320"></p>

# SecureSpeak 3.8

A TeamSpeak-style voice and text server with end-to-end encryption. There are no
accounts: you pick a nickname and connect. The server relays encrypted packets
and can never hear, read or fake anything.

| Component | Platforms | Files |
|---|---|---|
| Client | Windows 10/11 | `SecureSpeak-Setup-<version>.exe` installer (or the portable exe) |
| Client | Ubuntu, Linux Mint, Debian (amd64/arm64) | `linux-client/securespeak_3.0.0_amd64.deb`, or the portable binary + `install-client.sh` |
| Server | Linux (amd64/arm64) | `linux-server/securespeak-server` + `install-server.sh` |

## What's new in 3.8

- **10 color themes** (*Settings → Appearance*), based on the logo: SecureSpeak Navy (the new default), Midnight Shield, Bandana Red, Pin-up Retro, Espresso, Deep Ocean, Steel, Graphite Classic (the original look), and two light themes, Silver Rim and Pin-up Daylight.
  - Each theme has a live preview.
  - Picking one applies it at once, and it's remembered.

## What's new in 3.7

- **Server password in Edit server:** set, change or remove it in the same window as the server name and welcome message.
- **Choose the quality when you share:** clicking **Share screen** asks for Sharp, Smooth, Gaming or Custom and whether to use the GPU, then lets you pick the screen. Your last choice is remembered.
- **Server logs** (*Admin → Server logs*):
  - **What you see:** the last 5000 log lines, kept across restarts.
  - **Tools:** live updates, a filter, and a download button.
  - **Where they're stored:** `/var/lib/securespeak/logs/`, rotated at 5 MB.
  - **Who can see them:** a new permission, *View the server logs*. Roles that can edit the server get it automatically.

## What's new in 3.6

- **Screen share quality** (chosen when you start sharing):
  - **Sharp:** 15 fps, for text and code.
  - **Smooth:** 30 fps, the new default.
  - **Gaming:** 60 fps at 10 Mbit/s.
  - **Custom:** 5–60 fps, 720p up to 4K or your screen resolution, and 1–25 Mbit/s.
- **GPU encoding:** screen shares are encoded as H.264 on the graphics card (NVIDIA NVENC, Intel Quick Sync, AMD AMF), so the processor stays free. Without a suitable GPU, or with the option off, SecureSpeak uses VP9 in software.
- **Works with older viewers:** viewers tell the sharer which formats they can decode. If someone on an older version watches, the share automatically switches to VP9 so everyone can see it.
- **Bandwidth:** each viewer receives the chosen bitrate through the server. For example, 10 Mbit/s is about 4.5 GB per hour per viewer.

## What's new in 3.5

- **Automatic reconnect.** If the connection drops (internet trouble, or the server stopping or restarting for an update), SecureSpeak reconnects by itself and puts you back in your channel. A password-protected channel works too, if you joined it in this session.
  - It retries after 1, 2, 5, 10, 20 and 30 seconds, then every minute.
  - The chat shows just one line ("Connection lost · reconnecting automatically"), and the status bar shows the countdown. **Try now** and **Cancel** are on the start screen.
  - It doesn't reconnect after you disconnect yourself, a kick or a ban, a wrong password, or a changed server certificate. You can switch it off in *Settings → Profile & security*.
- **No more "ghost" users.** When you reconnect, the server replaces your old connection right away instead of showing you twice until it times out. A second copy of the same identity takes over from the first, and the first doesn't fight back.

## What's new in 3.4

- **Watch YouTube together:** click **Watch together** and paste a YouTube link.
  - Everyone in your channel gets an invite and watches in sync. The person who started the video controls play, pause and seeking, and everyone has their own volume.
  - The video plays in YouTube's official player, so YouTube's ads apply. Click **YouTube sign-in** to sign in to YouTube in the SecureSpeak window: with YouTube Premium the player shows no ads.
  - Who can start a video is set by the new permission *Watch YouTube together* (on for everyone by default).
  - The sync messages are end-to-end encrypted. The video itself streams from YouTube, which sees who watches what, just like on youtube.com.
- **Your own entrance and exit sounds:** in *Settings → Sounds*, upload a `.wav` or `.mp3` (at most 512 KB and 10 seconds). Others hear it when you connect to or leave the server.
  - The sounds are sent end-to-end encrypted, only to people who don't have them yet, and kept by everyone after that.
  - Anyone can switch other people's sounds off.
- **Leave message**, like TeamSpeak: "Sanne disconnected (Goodnight!)". It's sent end-to-end encrypted when you disconnect or quit.
- **Replace the app's own sounds** (connect, message, poke, someone joins and more) with your own `.wav` or `.mp3`. This only changes the sounds on your computer.

## What's new in 3.3

- **Channel icons:** right-click a permanent channel and choose **Set channel icon…**, or click **Icon** in the channel info.
  - **Built-in icons:** pick one of 27, such as gaming, music, AFK, staff or announcements.
  - **Your own icon:** upload a `.png` or `.ico`. Transparency is kept, and images are scaled down to at most 64×64 pixels.
  - **Who can set them:** only roles with the new permission *Set channel icons*. Guests don't have it by default, Server Admins and Moderators do, and you can change this in **Roles & permissions**.
  - **Stored on the server:** channel icons are visible to everyone on the server and stored by the server, like channel names. Uploads are checked and re-encoded by the server, so only plain image pixels are stored.
  - **Temporary channels** don't get icons.

## What's new in 3.2

- **New look (3.2.1):** a new logo, and a microphone-shield app icon used for the Windows installer, the exe, the Start menu, the Linux menu and the window.
- **Automatic updates.** SecureSpeak checks GitHub for a new release when it starts and every 6 hours. When there is one, a notice offers **Install and restart**. SecureSpeak then closes, installs the update and opens again.
- **About SecureSpeak** (*Settings → About SecureSpeak*) shows your version and checks for updates as soon as you open it, the way Chrome and Brave do. It also shows what's new in the latest release. You can switch automatic checks off there.
  - **Checked before installing:** every download is checked against the release's `SHA256SUMS` (and the SHA-256 that GitHub reports for the file). Nothing is replaced if they don't match.
  - **Signed releases (3.2.2):** `SHA256SUMS` is signed with the project's release key (Ed25519, `SHA256SUMS.sig`). The app only installs updates with a valid signature, so even someone who took over the GitHub account couldn't push a fake update. See [Release signing](#release-signing).
  - **How it installs:** the Windows setup runs silently, the portable exe and the `~/.local` Linux binary are swapped in place, and the `.deb` is installed with `pkexec` (you're asked for your password).
- **Quit button.** The ⏻ button at the top right (or **Ctrl+Q**) disconnects, closes the window and stops SecureSpeak.

## What's new in 3.1

- **Screen share sound (3.1.1):** sound is shared too, as stereo Opus at 128 kbit/s, end-to-end encrypted. Viewers get their own volume slider and mute button.
  - **Windows:** in the sharing dialog, switch on **"Also share system audio"**. It's offered for whole screens and, in recent Edge versions, for windows. SecureSpeak asks Edge to leave its own playback (other people's voices) out of the shared sound, so viewers don't hear themselves.
  - **Linux:** the browser engine only supports sound for browser tabs, so screens and windows are shared without sound.
- **Screen share:** click *Share screen* to share a screen or window with your channel. Others click the red **LIVE** badge (or *Watch screen*) to watch. Video is VP9 (or VP8), up to 1080p at 15 fps, and **end-to-end encrypted and signed** like voice. The server only forwards it to the people actually watching and can't see it.
- **Paste and drop files:** press **Ctrl+V** to send a copied screenshot or file, or drop files anywhere on the window. Pasted screenshots get a proper name (`Screenshot 2026-11-12 21.02.32.png`).
- **Image and video previews:** images (up to 16 MB) and MP4/WebM videos (up to 64 MB) load automatically and show inline in the chat. Click an image for the full-size view. Previews are kept in a private cache until you click **Save**. You can switch this off in *Settings → Profile & security*.
- **Server password from the app:** *Admin → Server password…* sets, changes or removes it, using the new permission "Change the server password". People who are connected stay connected.

## What's new in 3.0

- **Owner key instead of an admin password.** On first start the server prints a one-time owner key. Whoever redeems it becomes the permanent **server owner**. From then on they're recognized automatically by their identity key, with no password to type.
- **Roles and permissions** like TeamSpeak's server groups. Create as many roles as you like, each with a color, a **power** level and any of 24 permissions. Everyone gets the editable default role "Guest". "Server Admin" (power 100) and "Moderator" (power 50) are created for you.
- **User actions from the right-click menu:** Roles…, move, server-mute, kick from channel, kick from server and ban. Each one is shown only when you have the permission and the target has less power than you.
- **Privilege keys** hand out any role. You create one, give it to someone, and it works once.
- **Owner protection.** Nobody but the owner can kick, ban, mute, move or change the roles of the owner, whatever roles or permissions they have.

### What was new in 2.0
Opus voice, WebRTC noise suppression, whisper, E2E file transfer, avatars, signed voice packets (anti-spoofing) and the Linux client.

## Quick start

### Server (Linux)

```sh
cd linux-server
sudo ./install-server.sh        # systemd service; prints admin password + fingerprint
# or for a quick test:  ./securespeak-server -data ./data
```

On first start the server prints a **one-time owner key** like `SS-ABCD-EFGH-…`.
It's also stored in `data/owner-token.txt` until used, and
`securespeak-server -owner-token` shows it again. Connect with the client, click
**Privilege key** in the toolbar and paste it. You're now the owner.

Open **TCP and UDP 9987**. If UDP is blocked, voice falls back to TCP automatically.

`data/config.json` has these options: `name`, `welcome_message`, `max_clients`,
`allow_temp_channels`, `allow_whisper`, `allow_file_transfer`, and
`advertise_udp_port` for NAT. The `allow_*` switches are global: they
override role permissions. Useful flags are `-fingerprint`, `-owner-token`,
`-reset-owner` (if the owner lost their identity: it removes the owner and
prints a new owner key), `-set-server-password` and `-clear-server-password`.

**Upgrading from 3.0:** 3.1 is compatible with 3.0 apps and servers. Screen
share and changing the password from the app need the server updated to 3.1
as well. On first start, 3.1 adds the new permissions to existing roles:
"Share screen" for roles that can speak, and "Change the server password" for
roles that can edit the server.

**Upgrading from 2.x:** clients and servers must both be 3.0, because the
protocol now authenticates identities. The old admin password no longer
exists. On first start after the upgrade, the server creates an owner key; get
it with `sudo -u securespeak securespeak-server -data /var/lib/securespeak
-owner-token`. Channels, bans, config and the certificate are kept.

### Windows client

Download **`SecureSpeak-Setup-<version>.exe`** from the
[Releases](https://github.com/jmsuos/SecureSpeak/releases) page and run it.
- **Where it installs:** in your user profile (`%LOCALAPPDATA%\Programs\SecureSpeak`), with no administrator rights needed. Choose "Install for all users" in the first dialog to use Program Files instead.
- **What it adds:** a Start menu entry and an entry in *Settings → Apps*. A desktop icon and starting SecureSpeak when you sign in are optional.
- **Updating:** SecureSpeak updates itself (*Settings → About SecureSpeak*). Running a newer setup by hand also works. Either way your settings are kept.
- **Uninstalling:** this asks whether to also delete your settings and identity key. Keep them if you want to keep your identity and server roles.
- **Portable version:** `SecureSpeak-<version>-portable.exe` runs without installing.

The app opens in its own window using Edge, which ships with Windows. Allow
microphone access the first time. Settings and your identity key (encrypted
with your Windows login) are stored in `%APPDATA%\SecureSpeak`. Starting it
again while it's running just reopens the window.

Releases aren't code-signed yet, so Windows SmartScreen shows "Windows protected
your PC" the first time: choose *More info → Run anyway*.

### Linux client (Ubuntu / Mint)

```sh
sudo apt install ./securespeak_3.0.0_amd64.deb     # adds "SecureSpeak" to the menu
# or without root:  ./install-client.sh             (installs to ~/.local)
```

The window uses Chrome, Chromium, Edge or Brave when one is installed. Snap
versions work too. Otherwise it falls back to a dedicated Firefox profile.
Settings and the identity key live in `~/.config/SecureSpeak`, and received
files go to `~/Downloads/SecureSpeak`.

**Global push-to-talk on Linux:**
- **Linux Mint (X11):** works out of the box.
- **Ubuntu on Wayland:** Wayland doesn't let apps read keys globally. Run
  `sudo usermod -aG input $USER` and log out and in, after which SecureSpeak
  reads key state directly from the input devices. This lets programs running
  as your user see keyboard input, so only do it on a machine you trust.
- **Without either:** use the push-to-talk button in the window, or voice activation.

## Features

**Voice:** Opus, voice activation with a live meter, push-to-talk with release
delay, or always on. Mute, deafen, per-user volume (0–300%) and "mute for me".
Talking indicators, a jitter buffer, a limiter, device selection, a "hear
myself" test and notification sounds.

**Whisper:** whisper lists, made of users (remembered by fingerprint) and/or
channels (by name), each with an optional global hotkey and a toolbar button.
Right-click any user or channel to add it to a list, or hold "whisper" in a
user's panel. Purple rings and the status bar show who is whispering to you.

**Channels:** nested, with password, topic, description and user limit.
Permanent channels are made by admins; temporary ones by anyone, and they are
deleted when empty. Default channel, drag-and-drop moving.

**Text:** channel, server-wide and private chat, pokes and clickable links.
Links open in your normal browser, not inside SecureSpeak.

**Files:** use the paperclip button or drag files onto the chat. Recipients
click *Download*, transfers show progress, and finished files can be opened
with *Show in folder*. Received files are never run automatically.

**Roles & permissions** (Admin → Roles & permissions):
- **Roles tab:** create, edit and delete roles. Each has a name, color, power and permission checkboxes.
- **Members tab:** everyone who has a role, online or offline, with role editing.
- **Privilege keys tab:** create and revoke one-time keys for a role.

Right-click a user and choose **Roles…** to assign roles directly.

The permissions are:
- **Channels:** create temporary or permanent channels, edit or delete any channel, join any channel (ignoring password and limit)
- **Voice & chat:** speak, whisper, share screen, channel/server/private chat, poke, send files
- **Moderation:** move, kick from channel, kick from server, server-mute, ban, view ban list and unban
- **Administration:** assign roles, manage roles, create privilege keys, edit server name and welcome message, change the server password

The rules the server enforces:
- You can only act on users whose power is **lower** than yours.
- You can only hand out or edit roles below your own power.
- You can never grant a permission you don't have yourself.
- The **owner** has every permission and can't be moderated by anyone.

Bans block both the IP address and the identity key.

**Identity:** fingerprints, "verified" marks, and a warning when a known
nickname shows up with a different key.

## Security design

| What | How |
|---|---|
| Identity | Ed25519 key per installation. On Windows it is DPAPI-encrypted; on Linux it is a 0600 file. |
| Session | A fresh X25519 key per connection, signed by the identity key |
| Login / roles | Each connection proves possession of the identity key by signing TLS exported keying material, which is unique to that connection. Copying someone's public key or replaying their login gets you nothing. Roles are stored per identity key on the server. Privilege keys are 160-bit random, stored only as SHA-256 hashes, and work once. |
| Pairwise keys | X25519 → HKDF-SHA256 → AES-256-GCM |
| Voice | Opus frames are encrypted with **AES-256-GCM** using a random per-speaker key. That key goes only to channel members and is **rotated when anyone leaves**. Whisper uses a separate key sent only to the whisper targets, and a new one whenever the target set changes. |
| Voice authenticity | Every packet is **Ed25519-signed** over the sender id and ciphertext, and checked before decryption. The whisper flag is inside the encrypted part, so the server can't flip it. |
| Text, pokes, avatars | Each message gets its own random AES-256 key, wrapped per recipient and bound to the ciphertext, sender and recipient |
| Screen share | VP9/VP8 frames are encrypted with the speaker's AES-256-GCM channel key and **Ed25519-signed**, so only channel members can decrypt them and nobody can inject frames. They are relayed only to subscribed viewers in the same channel. |
| Previews | Served locally only if the content sniffs as PNG, JPEG, GIF, WebP, BMP, MP4 or WebM, with a sandboxing Content-Security-Policy. HTML and SVG never render. |
| Files | A random AES-256-GCM key per transfer *and* recipient, with chunk nonces bound to the transfer id. Flow control keeps at most 1.5 MB in flight. A SHA-256 check runs at the end. Names are sanitized, so paths and reserved names are stripped. |
| Transport | TLS 1.3 with the certificate pinned on first use. UDP datagrams carry a second AES-256-GCM layer with replay protection. |
| Local UI | The UI listens only on 127.0.0.1 with a random token, a strict Content-Security-Policy, Host and Origin checks, and no remote content |

**What the server can see:** IP addresses, nicknames, public keys, channel
membership, who talks or whispers to whom and when, who shares and watches a
screen, and file and frame sizes. It can't
see any voice, text, picture or file content.

**Verify fingerprints once** with the people you talk to, in person or by
phone. That rules out a server swapping in its own keys (a man-in-the-middle
attack).

## How the client is built

The Go core does networking, all cryptography, whisper and file logic, and
global hotkeys. The interface and audio run in an app window. Audio capture,
WebRTC processing, Opus encoding/decoding (WebCodecs) and playback (AudioWorklet)
happen there, and only encoded frames pass to the core over a local WebSocket.
This gives real Opus and high-quality noise suppression with no C libraries, and
the same audio code on Windows and Linux.

If a browser lacks the WebCodecs Opus encoder, which is only the case for very
old versions, the client falls back to 24 kHz ADPCM automatically.

## Building from source

Go 1.24+ only. No cgo and no third-party modules.

```sh
./build.sh            # all targets into dist/ (.deb needs dpkg-deb)
./build.sh linux      # only Linux client, server and .deb packages
go test -race ./...   # crypto, protocol and full client↔server tests
```

**Releases are built by GitHub Actions** (`.github/workflows/build.yml`).
- **Every push:** runs the tests and builds everything, including the Windows installer with Inno Setup on a Windows runner. The installer is smoke-tested by a silent install, launching the app, a single-instance check and an uninstall.
- **Publishing a release:** create a version tag. Either:
  - on GitHub: *Releases → Draft a new release*, type a new tag such as `v3.0.1` (target `main`) and publish; or
  - with git: `git tag v3.0.1 && git push origin v3.0.1`.

  Within a few minutes the workflow attaches the installer, portable exe,
  `.deb` packages and Linux tarballs to the release. Bump `VERSION` for the
  next development builds.

```
cmd/securespeak          client (Windows + Linux)
cmd/securespeak-server   server
internal/proto           wire protocol
internal/e2e             end-to-end crypto (identities, sender keys, signatures, files)
internal/server          server (roles.go: roles, permissions, privilege keys, owner)
internal/client          client core: sessions, keys, whisper, files, avatars
internal/hotkey          global hotkeys (Windows GetAsyncKeyState; Linux evdev / X11)
internal/platform        OS integration (DPAPI, app window, folders, links)
installer/               Windows installer (Inno Setup script, icon)
internal/ui              local bridge (HTTP, SSE, WebSocket) + web UI, audio engine, worklets
```

## Known limitations

- The Windows client has not yet been run on real Windows hardware by the
  developer. The audio path is shared with the tested Linux and Chrome build,
  but please report problems.
- File transfers go through the server live (nothing is stored), so the
  sender must stay online until recipients finish downloading.
- Not built yet: a server-side channel file browser, recording, a tray icon and
  a macOS client.

## Release signing

Updates are only installed when `SHA256SUMS` carries a valid signature by a
key listed in [`internal/update/signing-keys.pem`](internal/update/signing-keys.pem).
The release workflow signs it with the `UPDATE_SIGNING_KEY` Actions secret and
then checks the signature with the app's own code. Publishing a release fails
if the key is missing or doesn't match.

To check a download yourself (OpenSSL 3):

```sh
grep -v '^#' signing-keys.pem > key.pub     # the public key from the repository
base64 -d SHA256SUMS.sig > sig.bin
openssl pkeyutl -verify -pubin -inkey key.pub -rawin -in SHA256SUMS -sigfile sig.bin
sha256sum -c --ignore-missing SHA256SUMS
```

## License

SecureSpeak is free software: you can redistribute it and/or modify it under
the terms of the **GNU General Public License, version 3** as published by the
Free Software Foundation. See [`LICENSE`](LICENSE) for the full text.

In short: you may use, study, share and change it. If you distribute it, or a
modified version, you must provide the source code under the same license.

Copyright © 2026 jmsuos and SecureSpeak contributors.

Security problems: please report them privately as described in
[`SECURITY.md`](SECURITY.md).
