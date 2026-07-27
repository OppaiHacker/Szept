# 🔐 Security Model

> **Szept is designed so that the only party you need to trust is the person you are talking to.**  
> No server ever holds your keys, your messages, or your friend graph.

---

## 🔑 Identity

Every Szept user is an **Ed25519 keypair** — that's their identity, full stop.

- The key file is encrypted with **argon2id** (3 iterations, 64 MiB memory) + **XChaCha20-Poly1305**, stored with `0600` permissions.
- A human-readable fingerprint is shown in the UI for out-of-band verification.
- **Friend / invite codes embed the full public key** — adding a contact does not rely on trusting the server or TOFU. A MITM at the invite-code level would require tampering with the code outside the app.

---

## 🔒 Cryptographic Architecture

![Szept Security — Cryptographic Architecture Layers](assets/security_crypto_layers.jpg)

### Identity Layer
- **Keypair**: Ed25519 (signing & identity)
- **Key file at rest**: argon2id KDF → XChaCha20-Poly1305 envelope, `chmod 0600`

### Transport (P2P) Layer
- Connections: WebRTC **DTLS 1.2+** (via [pion](https://github.com/pion/webrtc)), NAT traversal via ICE/STUN.
- Signaling (SDP with DTLS certificate fingerprint) relays through the connector but is **Ed25519-signed by the sender and verified against the expected recipient's key** (+ freshness ±2 min, explicit addressee, tie-break for simultaneous offers).
- The connector **cannot substitute offers or perform MITM** — DTLS is cryptographically bound to the identity.
- **Result**: every data channel is an authenticated E2E pipe between two known identities. No TURN relay exists, so no intermediary ever touches payload bytes.

### Chat Events (DAG) Layer
- Chat history = a **DAG of events** (parents = SHA-256 hashes), each event:
  - **Ed25519-signed** by the author (signature covers ciphertext + full structure),
  - body encrypted with **XChaCha20-Poly1305** under the chat's **epoch key**, with `(chat, author, parents, epoch)` as AEAD associated data.
- History is **immutable**: modifying any node changes its hash and invalidates all descendants.

---

## 🌐 Transport Security

| Property | Mechanism |
|---|---|
| Peer authentication | Ed25519 signature on SDP, DTLS fingerprint binding |
| Channel confidentiality | DTLS 1.2+ (TLS record layer over UDP) |
| Signaling freshness | ±2 min timestamp window, replay deduplicated per window |
| Connector MITM | Impossible — DTLS cert is identity-bound |
| No TURN relay | Connector is tracker-only; content never transits it |

### Signaling Replay Protection

The same signed blob is accepted **only once** within a 2-minute window, so the connector cannot flood a client with copies of a single offer. Registration requires an Ed25519-signed timestamp; one socket handles one pubkey; claiming an occupied slot requires a _fresher_ signature than the one holding it — a replayed frame (possible on plain `ws://`) is insufficient.

---

## 💬 Chat Content Security

### DAG Authorization Model

Events are validated **before** being persisted. The fold rules are:

| Event type | Who may author it |
|---|---|
| `genesis` | Chat owner |
| `add` | Any current member |
| `remove` / `rekey` | Owner only |
| `msg` / `react` / `delete` | Any current member |

### Lamport Counter Ordering

DAG ordering derives from the **graph structure**, not from sender-supplied counters. Specifically:
- `lamport` must equal `max(parents)+1` exactly.
- A parent must be applied before its child.

Without this, an attacker-author could self-date an `add` event to appear before someone was removed — surviving a kick by antedating their membership.

### Epoch Key Distribution

- Epoch keys travel **exclusively over authenticated E2E DTLS channels** to verified members.
- Keys are **never overwritten**; a mismatched key produces a detectable AEAD failure.
- Keys are accepted **only from a current member** (during join — from the member who introduced the chat). A key that fails decryption is treated as absent: content becomes unreadable, but the chat continues operating.

### Member Removal & Key Rotation

Removing a member triggers automatic epoch rotation:
- The removed peer cannot decrypt new messages.
- Existing peers refuse sync with the removed peer.
- The removed peer's DTLS link is torn down.
- Removal **cascades** to all members that the removed peer introduced — otherwise an attacker could add a second identity to survive the kick.

Manual rotation: `/rekey`.

### Invitations

- The inviter stores only the **hash** of the 128-bit secret; the secret itself is single-use, enforced atomically.
- PINs have a **5-attempt limit** before the invitation is burned. Without this, 10⁶ combinations are brute-forceable in seconds over an open channel.

---

## 📎 Attachments & Avatars

![Szept Security — Attachment & Avatar Processing Pipeline](assets/security_attachment_pipeline.jpg)

### What Goes into the DAG

Only **metadata** is embedded in the signed, encrypted DAG event:
`sha256`, MIME type, filename, size, dimensions. Raw bytes travel separately, on demand, over the same E2E channel.

### File Type Detection

- Type is detected from **magic bytes** (`http.DetectContentType`) — never from the filename or a peer-supplied MIME field.
- **Allowed**: PNG, JPEG, GIF, WebP, MP4, WebM
- **Blocked**: HTML, SVG, scripts, archives, ELF, everything else

### Decompression Bomb Guards

Checked **before** full decode:
- Attachment max: **32 MiB**
- Avatar input max: **4 MiB**
- Max dimensions: **8 000 px**, **20 MPx**
- GIF: frame count limit

Video is never decoded — it is handed directly to the system player, so no decoder attack surface exists here.

### Integrity Verification

The receiver recomputes the SHA-256 of received bytes and compares it to the signed DAG event. The hash is known only from decrypted history, acting as an access token: without it, a peer cannot even request the file. Files are served **only to members of that chat**.

### Filename Sanitization

- Filename is generated by us: stripped to basename (no path traversal), control characters removed, extension **enforced by detected MIME type**.
- A file from the network cannot land on disk as `.sh` or `.exe`.

### Avatar Transcoding

- Decoded → center-cropped to **256 px square** → re-encoded as PNG (or animated GIF preserved).
- Only our encoder's bytes reach the database. Appended payloads (polyglot ZIP/HTML/JS), EXIF, ICC profiles, and comments do not survive the round-trip.

---

## 💾 Data at Rest

- The **entire database is encrypted with a storage key** — not just event bodies. The envelope (author's public key, event kind, epoch, graph links) is also encrypted, because that is conversation metadata. From the database file alone, you cannot reconstruct even the "who talked to whom and how often" graph.
- Only **record keys are plaintext**: event IDs (hashes), blob content-addresses, and friend public keys — all of which are hashes themselves.
- Every value is **bound to its location** (bucket + key as AEAD associated data): even with database file access, you cannot rearrange encrypted records — e.g., swap a blob under a given hash, or move an epoch key to a different chat.

| Item | Encryption key source |
|---|---|
| Event bodies | Chat epoch key → XChaCha20-Poly1305 |
| Event envelopes | Storage key (Ed25519 seed-derived) |
| Epoch keys, chat metadata, contacts | HKDF from identity seed |
| Blobs (attachments, avatars) | Storage key; content-addressed by SHA-256 |

> **⚠️ A password is optional, but without one there is no encryption at rest.**  
> An `identity.key` without a password can be unpacked by anyone who has the file; the storage key is derived from its seed, meaning the entire database is readable.

Opened attachments land **decrypted in the system temp directory** (the system player requires a plain file) and remain there.

---

## 🛡️ What the Connector Sees (Honestly)

![Szept Security — Connector Visibility vs Privacy](assets/security_connector_visibility.jpg)

### What it sees
- **pubkey ↔ IP** pairs of registered clients
- **Who signals whom** (connection-graph metadata)

### What it never sees
- Message contents
- Chat names or membership
- Epoch keys
- Attachment bytes or avatars

### Mitigations
- **Self-host the connector** (~200 lines, fully stateless) — you become your own tracker.
- Use **`wss://`** (TLS) — prevents passive eavesdropping on the signaling channel.
- Route through **Tor or a VPN** to decouple your IP from your pubkey at the connector.

---

## ⚖️ Known Trade-offs & Upgrade Paths

| Topic | Current state | Upgrade path |
|---|---|---|
| **Forward secrecy** | Sessions have FS (ephemeral DTLS). Chat history does **not** — epoch keys are retained so new members can read history (intentional product decision). | Frequent `/rekey` narrows the window. Double Ratchet (1:1 chats) is the natural upgrade. |
| **Offline delivery** | None — P2P by definition; no server means no mailbox. | Sealed-sender store-and-forward relay (opt-in). |
| **Connector metadata** | Connector sees the connection graph (like a torrent tracker). | Self-host + Tor/VPN (see above). |
| **Mesh voice/video** | Each participant sees all other streams (mesh). | SFU for large groups. |
| **Traffic analysis** | Packet size and timing can leak activity to a network observer. | DTLS-level padding (variable-length records). |
