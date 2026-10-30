# 🔐 Security Model

> **The only party you need to trust is the person you're talking to.**
> No server ever holds your keys, your messages, or your contact list.

---

## 🔑 Identity

Every user is an **Ed25519 keypair**. That's the entire identity.

- Key file at rest: `SZEPT2` magic + 16-byte salt + `crypt.Seal(argon2id(passphrase, salt, 3 iter, 64 MiB, 2 threads), seed || storage key || name)`. Written to a temp file and renamed, so a crash never leaves half a key file. `SZEPT1` files (no storage key) still load and use the old seed-derived key until the next passphrase change.
- Stored `0600`. Fingerprint = SHA-256 of pubkey → base32, displayed as `XXXX-XXXX-XXXX-XXXX`.
- Friend and invite codes **embed the full public key** — no TOFU, no trusting the server. MITM at invite time requires tampering with the code outside the app.
- Storage key: 32 random bytes kept next to the seed in the key file. Not derived from the seed, so a passphrase change can replace it (see Data at Rest).

### Names are hints; the key is the identity

- **A friend code carries no name.** It is `{pubkey, connector hint}` only. Codes travel through screenshots, QR photos and clipboards, and a name in there is free metadata for anyone who sees the picture. Until a name arrives, the contact shows as the first 9 characters of the fingerprint.
- The display name arrives over the **already-authenticated E2E link** (`hello`), and is accepted **only for someone already on your friend list**, after `safeName()` (control characters stripped, ≤ 64 B, single line). A stranger cannot rename anybody.
- Inside a chat, names live in the **signed DAG**: the genesis event carries the creator's, every later change is a `nick` event. `NickHistory()` replays them, so a peer who swaps their display name mid-conversation leaves a visible trail — the profile screen lists every name that person has announced, oldest first.
- The fingerprint is always one tap away and is the only thing worth comparing out of band.

---

## 🔒 Cryptographic Architecture

![Szept Security — Cryptographic Layers](assets/security_crypto_layers.jpg)

### Layer 1: Identity

| Primitive | Usage |
|---|---|
| Ed25519 | Keypair = identity; signs events, SDP offers, registrations |
| argon2id | Passphrase → key derivation (3 iterations, 64 MiB, 2 threads) |
| XChaCha20-Poly1305 | Encrypts key file at rest |
| HKDF-SHA256 | Subkey derivation (signaling seal keys, `SZEPT1` storage key) |

### Layer 2: Transport (P2P)

- Connections: **WebRTC DTLS 1.2+** via [pion/webrtc](https://github.com/pion/webrtc), NAT traversal via ICE/STUN.
- SDP offers contain the DTLS certificate fingerprint **and a 32-byte ChaCha20 obfuscation key** (`sigPayload.Obf`). The whole signed offer/answer is sealed to the addressee (ephemeral X25519 + XChaCha20-Poly1305, `p2p/seal.go`), so a connector sees only the sender and addressee keys, not the SDP, candidate IPs or the obfuscation key.
- Every offer is **Ed25519-signed by the sender**, and the receiver verifies:
  1. Signature against sender's known pubkey
  2. `From` matches the connector's `from` stamp
  3. `To` matches our own pubkey (explicit addressee)
  4. Timestamp within ±2 minutes (freshness)
  5. `kind + from + connID` not seen before (replay filter)
- **Glare handling**: simultaneous offers → lower pubkey's offer wins, loser abandons.
- **One pending answer per key**: a newer offer from the same key closes the PeerConnection we opened for the previous one, so an offer flood costs one PeerConnection, not one per offer.
- A link in `Disconnected` (switching Wi-Fi to LTE) is kept; ICE may recover it, and only `Failed` or `Closed` tear it down.
- **TURN relay is optional** (see Relay below). Without it, payload bytes never transit the connector; through it, they transit as DTLS ciphertext.
- Result: every data channel is an authenticated, encrypted E2E pipe between two known identities.

### Layer 3: Chat Events (DAG)

Each event in the chat DAG:
- Has its body encrypted with **XChaCha20-Poly1305** under the current **epoch key**, using `signable(chat, kind, author, lamport, epoch, parents)` (with empty Box) as AEAD associated data.
- Is **Ed25519-signed** — signature covers the signable bytes including ciphertext.
- Has its **ID = SHA-256(signable + signature)** — content-addressed and tamper-evident.

History is **immutable** — modifying any event changes its hash and breaks every descendant.

---

## 🧅 Cebulion Network & Connector Security

### Standalone connector

A single connector works as a stateless signaling relay (~200 lines). It maps `pubkey → WebSocket` and forwards opaque blobs. It stamps `from` from the authenticated registration — clients cannot spoof the sender field.

### When the connector is needed

The connector is a rendezvous point, not a transport. It is needed **while a link is being set up**, never while you talk.

| Connector | What works | What does not |
|---|---|---|
| Unreachable, link already up | Everything on that link: messages, files, calls, blob transfers | — |
| Unreachable, no link to that peer | Local reads and writes — an event is appended to your own DAG and gossiped to peers you are already connected to | Reaching a peer you have no link to. No `lookup`, no signalling, so no new DTLS handshake |

- `ensurePeer` asks `lookup` whether the peer is online and only then dials through `Signal`; both go over the connector socket.
- **There is no fallback rendezvous.** ICE mDNS is disabled on purpose so no cleartext `.local` multicast leaks alongside the obfuscated flow, and there is no direct-IP dial. The connector — or the Cebulion mesh behind it — is the only way two clients find each other.
- Links drop for ordinary reasons: sleep, network change, NAT rebind. So a connector has to be reachable most of the time even though not one message byte passes through it.
- Recovery needs no user action: `Client.Run()` reconnects with exponential backoff (1 s → 30 s), `connectAll` re-dials known friends and chat members every 30 s, and a restored link syncs the DAG from its heads.
- Nothing written offline is lost. Events wait in the local DAG and travel over `sync`/`want` once the link is back — but delivery still needs **both** sides online at the same time. There is no mailbox; see Known trade-offs.

### Cebulion mesh

Connectors can join the **Cebulion Network** — a distributed mesh providing fault tolerance and load balancing.

**The mesh is open.** Any connector can join by dialing a node it knows and sending `mesh_hello`. A shared secret would not keep anyone out of a volunteer network, because everyone has to get it, the censor included. So the design assumes hostile nodes and limits what they can do. `--mesh-secret` turns the mesh private: then `mesh_hello` must carry `HMAC-SHA256(secret, "szept-mesh-v1" || timestamp)` inside a ±60 s window. `--mesh=false` disables it. Ordinary client sockets that never say `mesh_hello` cannot reach mesh operations.

| Mechanism | Details |
|---|---|
| **No public endpoint** | The connector upgrades only on its secret path. Everything else, including the old `/ndp/neighbors` and the status page, gets a 404 or the `--decoy` site. Gossip runs inside the mesh WebSocket. |
| **Gossip rationing** | An `ndp_gossip` reply and a `redirect` carry at most 8 and 3 URLs, chosen by a hash of the asker's /16 (IPv4) or /48 (IPv6) keyed with a per-process random salt, so nobody can work out in advance which block would see which nodes. Walking the mesh from one network block shows a few nodes, not the full list a censor needs to block it. A node answers gossip for at most 32 network blocks per hour; neighbours re-ask every 15 s and keep their slot, while a censor crawling from many address ranges gets empty answers once the slots are taken. This raises the cost of enumeration. **`--private-gossip`** takes a node out of it entirely: it answers gossip with nothing (except to peers proven by `--mesh-secret`), sends clients no redirect list, and its `mesh_hello` carries `hidden`, so honest neighbours never pass its URL on. Such a node joins only through its own `--seeds`; a hostile neighbour can still ignore the request and leak the URL. No setting stops a censor who already knows a node from blocking that node, which is true of every open volunteer network (Tor bridges included). |
| **Neighbour URLs** | `ws://` / `wss://` only, max 128 chars, and never a non-public address: loopback, RFC1918, link-local (incl. `169.254.169.254`), multicast and unspecified are all refused. Hostnames are resolved, **every** answer must be public, and the socket is opened to the checked address, so DNS rebinding does not get through. A URL learned from gossip is a candidate until it answers our own dial; when the table is full, an unreached entry makes room, never a live one. |
| **Lookup and routing** | A `lookup` for a key this connector does not host goes out as `mesh_lookup`. The hosting connector replies `mesh_found`, which walks back along the trace; each node on the way records which neighbour leads to the key (5 min). An answer for a lookup that never passed through a node is ignored. |
| **Relaying** | A `mesh_signal` goes to the remembered neighbour plus up to 4 others, so a node that lies in `mesh_found` and drops frames cannot blackhole a signal. Each neighbour has one socket with a reader and a 64-frame queue; a full queue drops. |
| **Delivery receipts** | The connector that hands a `mesh_signal` to its client answers `mesh_found` on the same trace, which walks back to the sender's connector and teaches every node on the way the route. No receipt within 2 s: the signal goes out once more to **every** neighbour. Still none: the sender's client gets `undelivered` and drops its pending offer at once, so the next connect round retries instead of waiting 30 s for an answer that will not come. A standalone connector reports `undelivered` straight away for keys it does not host. A hostile node on the path can fake a receipt, as it can fake a lookup answer; the flood's other branches still carry the frame. |
| **Rate limits** | Per socket: 60 `lookup`, 120 `signal` and 1200 mesh frames per minute. |
| **Loop prevention** | Each mesh frame carries a random 16-char hex `TraceID` (max 64 chars accepted) and `TTL` (5, decremented per hop). Seen traces live in a ring of 20 000 entries; a flood evicts old entries instead of freezing relaying. |
| **`from` on a relayed frame** | A routing hint, not an identity. Only the Ed25519 signature inside the payload (`p2p.onSignal`) says who sent something. |
| **Load shedding** | When `--max-peers` is hit, new clients get a `redirect` message with 3 neighbour URLs and the connection closes. Client appends these to `FallbackURLs` and retries. |
| **Client failover** | `Client.Run()` reconnects with exponential backoff (1s → 30s, reset after 15s uptime). Tries primary URL, then all `FallbackURLs` in order. A `redirect` is **validated before use**: no `wss://` → `ws://` downgrade, no duplicates, at most 8 fallbacks total. Changing the connector in Settings builds a fresh client, so an old fallback list never survives the switch. |
| **DNS resilience** | On `no such host`: fallback to `1.1.1.1:53`. If port 53 blocked: DNS-over-HTTPS to `https://1.1.1.1/dns-query` (Cloudflare cert has IP SAN, so DNS is not required to query DNS), with a 10 s timeout. |

### Registration security

- Registration requires Ed25519 signature over `"szept-register-v1" || pubkey || timestamp(BE64)`.
- Timestamp must be within ±1 minute.
- One socket = one pubkey. Claiming an occupied slot requires a **fresher timestamp** — replaying a captured frame is not enough.
- Per-IP session limit: **64 concurrent WebSockets** per IP address, taken **before** the upgrade — including on the overloaded `redirect` path, which used to hand out unlimited handshakes.
- Behind a reverse proxy every connection arrives from loopback. The client IP is read from the last `X-Forwarded-For` entry (the one the proxy appended; earlier entries are client-supplied) **only** with `--trust-proxy`; without the flag loopback counts like any other address, because an unconditional exemption disabled the limit entirely.
- `lookup` is rate-limited to 60 queries per minute per session, `signal` to 120. It still answers for any pubkey — see Known trade-offs.
- The WebSocket upgrade **rejects any request carrying an `Origin` header**. Native clients send none; a browser always does, so a web page cannot open a socket to the connector (CSWSH).
- `ReadLimit`: 256 KB per WebSocket message. `ReadHeaderTimeout`: 10s (Slowloris mitigation), `IdleTimeout`: 60s for keep-alive connections.
- A key that registers again (new socket) closes its old socket, so a stale session cannot linger.

### Switching connectors, and dynamic connector lists

Settings keeps a **saved list of connector URLs** to switch between, and optionally a set of **list endpoints** — `https://` addresses returning one `ws://`/`wss://` URL per line.

| Guard | Value |
|---|---|
| Scheme | `https://` only: a list fetched over plain HTTP tells DPI that a Szept client is asking |
| Redirects | Not followed, so a list host cannot bounce the request into your local network |
| Response size | 64 KB per list, hard cap |
| Timeout | 8 s per request |
| Accepted lines | Only those starting `ws://` or `wss://`; everything else dropped |
| Effect of a refresh | New URLs are **appended to your saved list — never connected automatically** |

![Settings — connector and lists](assets/shots/settings-connectors.png)
*Saved addresses and list endpoints are both empty by default; a refresh only appends URLs to the saved list.*

What a list host learns: your IP and the fact that a Szept client asked, exactly like any web request. What it can do: offer connectors it controls, which — if you then pick one — puts it in the same position as any connector (`pubkey ↔ IP` and who signals whom). What it cannot do: read messages, keys, membership, or impersonate anybody. Use lists you'd trust with your IP, or don't use them at all — the feature is opt-in and empty by default.

### What the connector sees (honestly)

![Szept Security — Connector Visibility](assets/security_connector_visibility.jpg)

| Visible | Never visible |
|---|---|
| `pubkey ↔ IP` mapping | Message contents |
| Who signals whom | Chat names & membership |
| Connection timestamps | Epoch keys |
| Neighbour connectors and their RTT | Attachments, avatars & banners |
| Relayed traffic volume and its endpoints (TURN) | Contents of relayed traffic (DTLS end to end) |
| | Friend relationships |
| | Profile status / description |
| | DAG structure or event history |

---

## 💬 Chat Content Security

### Authorization rules

Events are validated **before** persistence (`ingest` → `Fold`):

| Event kind | Authorized author |
|---|---|
| `create` (genesis) | Chat owner (exactly one per chat) |
| `add` | Any current member — **but only for a pubkey not already in the chat** |
| `remove` / `rekey` | Owner only |
| `msg` / `file` / `react` / `delete` | Any current member |
| `avatar` / `nick` / `profile` | Any current member — **own profile only** |
| `rename` | Owner only — a member's rename is ignored |

Validation: event size (256 KB max) → signature check → parent presence → the event is applied to the chat's cached fold (a full replay of the DAG only when the event lands in the middle of history or the epoch keys changed) → authorization failure rejects the event and leaves the cached fold untouched. One `events` frame carries at most 2 000 events, mirroring the outbound cap.

`add` **adds; it never rewrites an existing row.** Overwriting used to let any member resurrect an account the owner had kicked, rename the owner inside signed state, and — by rewriting the owner's `AddedBy` — make the owner's next kick cascade onto themselves and lock the chat for everyone. Re-admitting a removed member is the owner's decision alone.

`Fold` applies profile-shaped events by looking up `st.Members[e.Author]` — the author's own row and nothing else. There is no addressing field to point one somewhere else, so "change someone else's status" is not an operation the protocol can express.

### Lamport ordering

DAG ordering comes from the graph, not from sender-supplied values:
- `Lamport` must equal `max(parents) + 1` exactly.
- A parent must be in `seen` (applied) before its child.
- Genesis event: no parents, no Lamport check.

This prevents antedating attacks — e.g. inserting an `add` at Lamport 2 that appears before a `remove` at Lamport 3, or a `Lamport = 2^64-1` that rolls the counter and blocks future events.

### Epoch keys

- Epoch keys travel **only over authenticated E2E channels** (DTLS ctrl channel) to verified members.
- A stored key is **replaced only when signed history proves it wrong and the offered key right** (`keyVerdict`). One fake key therefore cannot lock its epoch forever, and a fake key can never displace a correct one.
- **Each epoch key is vouched for by the signed DAG.** `KindCreate` and `KindRekey` carry `sha256(key)` of the epoch they open, and an offered key is stored only when its hash matches. Without this binding, a member could pre-fill epochs that did not exist yet; the real key was then locked out permanently.
- Chats created before that binding existed have no announced hash. For them the fallback rule applies: **a key for an epoch the signed history has not announced yet is refused** (`ep > st.Epoch` → drop), which closes the pre-poisoning window; the remaining race narrows to the moment a rekey is published.
- A key that fails AEAD decryption is treated as absent. Only the genesis event is fatal — every other unreadable event is **skipped and its epoch flagged** (`State.MissingKey`), so one bad or not-yet-delivered epoch key degrades the view of the chat instead of destroying it. Membership events in a flagged epoch are parked and the epoch key re-requested rather than applied blind.
- A degraded state is honest about the cost: a removal you cannot read is a removal you have not applied, exactly like one that has not reached you yet. What it stops is the previous behaviour, where a single unreadable epoch made `ChatState` fail outright — the conversation then vanished from the chat list and refused every sync, key, blob and append, with no way back short of editing the database.
- **Two keys per epoch.** Membership and profile events (`create`, `add`, `remove`, `rekey`, `rename`, `nick`, `avatar`, `profile`) are sealed with the epoch's *member key*, `HKDF(content key, "szept-member-v1")`; messages, files, reactions and deletes with the content key itself. The derivation is one-way, so a member key reveals no content. `KindCreate` / `KindRekey` also carry `sha256(member key)`, so a member key held alone is checked against signed history the same way (`mkeyVerdict`). A content event whose key is missing still has its author checked against membership, which needs no body.
- **Chats without history** (`NoHistory`, set in the genesis event, the default for new groups in every client). A member added later gets member keys for every earlier epoch, enough to fold who is in the chat and what they call themselves from genesis, and content keys only from their `JoinEpoch` on: the epoch after the one they were added in. The owner rotates the key right after every add (`autoRekey`: when it redeems the invite, or when it syncs an add made by someone else), so the newcomer's first content key was never used for anything written before they joined. Until the owner has done that, the newcomer can read nothing and cannot write. A member who removes and re-adds someone does not reopen the old epochs either. Honest members follow `JoinEpoch` when they push keys; a dishonest member can always leak keys or plaintext, which no scheme prevents.
- Keys are accepted **only from a current member** or, during join, from the inviter / a friend who introduced the chat. While joining there is no folded state yet, so the genesis event (authenticated by its ID, which is the chat ID) judges the epoch-1 key: a genesis that does not decrypt is kept, the key is re-requested, and only a key that decrypts it is accepted.

### Member removal & cascade

Removing a member (`KindRemove`, owner only):
1. `removeTree(st, pub)` — marks the member `Removed = true`.
2. **Cascades** to every member whose `AddedBy` is the removed pubkey — recursively.
3. Triggers epoch rotation (`KindRekey`) — the removed peer can't decrypt new messages.
4. Existing peers refuse sync/want/events from non-members.
5. The removed peer's DTLS link is torn down via `Accept()` returning false.

Without the cascade, an attacker adds a second identity before being kicked — that identity survives and keeps receiving epoch keys.

### Invitations

- Invite code = base64 JSON: `{ chat_id, inviter_pubkey, 128-bit_secret, connector_url, expiry }`.
- PIN = 6 random digits (`crypto/rand`). Inviter stores `SHA-256(secret)` and `SHA-256(pin)`.
- **5-attempt limit** on PIN before the invite is burned (`TakeInvite` atomic). Without this, 10⁶ combinations are brute-forceable in seconds.
- Invite TTL: **10 minutes**. Inviter must be online for redemption.
- A stranger's offer is accepted only if it carries `SHA-256(secret)` of an open invite (`sigPayload.Inv`, inside the sealed signal). Without it the inviter does not answer, so an open invite does not reveal the inviter's ICE candidates (IP) to anyone who knows their public key. The redeemer may use any connector: the inviter is found through the mesh.
- Only the **chat owner** can issue invites — preventing members from unilaterally admitting strangers into shared encrypted history.

---

## 🪪 Profile Card (status, description, banner)

![Szept Security — Profile Card Flow](assets/profile_card_flow.svg)

![Profile card as a peer sees it](assets/shots/profile-card.png)
*Status, description and banner as they arrive at a peer — one signed `profile` event, encrypted under the chat's epoch key.*

A profile card is a `profile` event: status line, description and banner travel **together**, so a peer who missed an earlier event still ends up with a consistent card instead of a half-updated one. An empty field clears that field.

| Guard | Value |
|---|---|
| Status | `safeName()` — single line, control characters stripped, ≤ 64 B |
| Description | `safeBio()` — newlines kept, other control characters stripped, ≤ 400 B |
| Banner | Full attachment pipeline (below): magic-byte sniff, bomb guards, re-encode |
| Author | Applied to the author's own member row only — never to anyone else's |
| On arrival | Text is cleansed **again on ingest**, before it reaches the database or a widget |

Cleansing happens on both ends deliberately: `SetStatus`/`SetBio` protect other clients from us, `noteAttachment` protects us from every other client. Text arriving from the network is never trusted because it was checked by whoever sent it.

**Visibility, stated plainly:** the card is emitted into **every chat you are a member of**. Anyone sharing a group with you sees your status, description and banner — including people you never added as friends. It is a profile, not a secret; treat it like one. The connector still sees none of it: the card is encrypted under each chat's epoch key like any message.

---

## 📎 Attachments, Avatars & Banners

![Szept Security — Attachment Processing Pipeline](assets/security_attachment_pipeline.jpg)

### What enters the DAG

Only metadata: `sha256(content)`, MIME type, filename, size, dimensions, thumbnail hash — inside the signed, encrypted event body. Raw bytes travel separately, in 16 KB chunks, over the same E2E ctrl channel as binary frames (a tag byte that cannot start JSON, then chat, hash, index, total and the bytes; no base64).

### Processing pipeline

| Stage | What happens |
|---|---|
| **Type detection** | Magic bytes (`http.DetectContentType`), never filename. Allowed: PNG, JPEG, GIF, WebP, MP4, WebM. Everything else rejected. |
| **Bomb guards** | Checked before full decode: 32 MiB attachment / 4 MiB avatar and banner input, 8 000 px per side, 20 MPx, GIF frame count limit. |
| **Avatar transcode** | Decode → crop → 256 × 256 → re-encode PNG (animated GIF preserved, ≤ 1 MiB out). |
| **Banner transcode** | Decode → crop → 720 × 240 → re-encode PNG, or GIF for an animated one: ≤ 48 frames, ≤ 2 MiB out, frames decimated until it fits. |
| **Crop rectangle** | Comes from the UI, therefore from the user: intersected with the source bounds, degenerate values fall back to the whole image, aspect trimmed rather than stretched. Never used as given. |
| **Re-encode, always** | Only our encoder's bytes reach the database — EXIF, ICC profiles and polyglot payloads appended after the image data do not survive the round trip. |
| **Integrity check** | Receiver computes SHA-256 of received bytes vs. the hash in the signed DAG event. |
| **Access control** | Serving a blob needs **both**: the asker is a member of the chat they name, and that chat is one the blob was actually announced in (`blobchat` index, rebuilt on startup). Membership alone was not enough — blob storage is content-addressed and global, so any shared chat would otherwise have been a pass to files from every other chat, and a guessable hash a confirmation oracle. |
| **Inbound chunks** | Accepted only from an active member of the chat the download belongs to, and only for a blob we asked for. A chunk claiming a different `Total` is **dropped**, not acted on — rebuilding the buffer discarded hundreds of already-fetched chunks on one crafted packet. |
| **Filename** | Generated locally: basename only (no path traversal), control characters stripped, extension **forced by detected MIME**. A network file cannot land as `.sh` or `.exe`. |
| **Resume** | A request for a half-assembled file carries the indices still missing, and the sender sends only those. While parts keep arriving, no new request goes out. |
| **Transfer control** | Max 3 concurrent blob transfers per peer (`maxServing`). Total assembly memory capped at 64 MiB (`asmBudget`). Send buffer pauses at 1 MiB high water mark. |

An animated banner is also a **memory** decision, not only a size one: every frame is held unpacked while it plays (720 × 240 RGBA ≈ 700 kB), which is why the frame cap exists and is low.

Video is never decoded — it's handed to the system player. No decoder attack surface.

Large files (>10 MB) replicate as a preview thumbnail only. The full payload stays at the sender and travels only when the recipient explicitly requests it.

---

## 🖥️ Local Surface

Everything above defends the wire. This section is about the machine the app runs on — where the defence is file permissions and one token.

![Szept Security — Local Attack Surface](assets/security_local_surface.svg)

### Webhook bridge for bots

![Szept Security — Local Webhook Bridge](assets/webhook_bridge.svg)

Off by default. When enabled it binds **`127.0.0.1` only** and accepts one route:

```
POST /send   Authorization: Bearer <token>   {"chat": "...", "text": "..."}
```

| Guard | Value |
|---|---|
| Bind address | `127.0.0.1:<port>` — never `0.0.0.0`, so it is not reachable off the machine |
| Token | 32 bytes from `crypto/rand`, base64url, compared with `crypto/subtle.ConstantTimeCompare` |
| Body | `http.MaxBytesReader`, 64 KB |
| Headers | `ReadHeaderTimeout` 5 s |
| Lifecycle | Stopped on `Core.Close()` and before an account switch, so the port is free for the next process |

![Settings — bots](assets/shots/settings-bots.png)
*The whole local surface: one toggle, one port on `127.0.0.1`, one bearer token. "New token" invalidates the old one immediately.*

**What this genuinely means:** any process on your computer that can read `config.json` can send messages that are signed by your key and indistinguishable from ones you typed. That is the intended feature (that's how you write a bot), and it is also its whole risk. Leave it off unless you use it; "New token" invalidates the old one immediately.

### Files

| Path | Mode | Contents |
|---|---|---|
| `~/.local/share/szept/identity.key` | `0600` | Seed + storage key + name, encrypted **only if you set a passphrase** |
| `~/.local/share/szept/szept.db` | `0600` | Fully encrypted database (random storage key from `identity.key`) |
| `~/.local/share/szept/profiles/<name>/` | `0700` | A second account: its own identity + database |
| `~/.config/szept/config.json` | `0600` | Connector list, UI preferences. No secrets: the webhook token lives in the encrypted database |
| `<data dir>/tmp/` | `0700` | Attachments opened with "Open" are written there **decrypted**. The directory is wiped on exit and again at every start, so a crash or a killed process leaves nothing behind past the next launch |

---

## 👥 Accounts & Backups

![Szept Security — Accounts and Backups](assets/accounts_backup.svg)

### Accounts (profiles)

![Settings — accounts](assets/shots/settings-accounts.png)
*Each account is its own directory with its own identity and database; the page also shows the active data directory, including when `-data` overrides the choice.*

- An account is a directory. Separate identity, separate database, separate contacts and history — nothing is shared between them.
- The active account is recorded in `config.json`; the unlock screen and Settings → Accounts both switch it.
- Switching **relaunches the process** rather than swapping a live `Core`. A live swap would leave background goroutines writing to a closed database; a restart is boring and correct.
- A `-data` flag overrides the account choice, and the settings page says so instead of silently ignoring your click.
- Deleting an account deletes its identity file and its database. Irreversible, no copy elsewhere.
- Android keeps a single account — the platform already sandboxes the app per install.

### Backup & restore

- Backup writes a zip with `identity.key` and a **live bbolt snapshot** (taken inside a read transaction, so it is consistent without closing the app).
- The backup is exactly as protected as your identity passphrase: the database key sits in `identity.key`, which the passphrase encrypts. **No passphrase means a backup anyone can open.** A backup made before a passphrase change keeps the old passphrase and the old database key; it opens only its own copy of the database.
- Restore reads **exactly two entry names** (`identity.key`, `szept.db`), each exactly once; an archive with a duplicate entry is rejected. Every other entry — including anything with `../` in its path — is skipped in full, so a hand-crafted archive cannot write outside the destination.
- Restore never touches the running database: it unpacks into a **new account directory**, which you then switch to.
- Entry size is bounded to 512 MB and **streamed to disk** (`io.Copy`), so a crafted archive cannot be inflated into RAM before anything checks it.

---

## 💾 Data at Rest

The **entire database is encrypted** — not just message bodies. Event envelopes (author pubkey, event kind, epoch, graph links) are encrypted too, because they are conversation metadata. From the database file alone, you cannot even reconstruct a "who talked to whom" graph.

| Data | Key source |
|---|---|
| Event bodies | Chat epoch key (XChaCha20-Poly1305) |
| Event envelopes | Storage key (random, kept in `identity.key`) |
| Epoch keys, chat metadata, contacts, webhook token | Storage key |
| Profile index (avatar, banner, status, description) | Storage key — a cache of what the signed DAG already says |
| Blobs (attachments, avatars, banners) | Storage key; content-addressed by SHA-256 (same image in 5 chats = 1 copy on disk) |

Only **record keys are plaintext**: event IDs (hashes), blob content-addresses, friend pubkeys — all hashes themselves.

Every value is **bound to its storage location** (bucket + key as AEAD associated data) — you can't rearrange encrypted records, e.g. swap a blob under a different hash or move an epoch key to another chat.

![Settings — passphrase and backup](assets/shots/settings-pass.png)
*The passphrase is the only thing standing between a copied `identity.key` and the whole database — the backup zip inherits exactly that protection.*

> ⚠️ **A password is optional, but without one there is no encryption at rest.** Every client (CLI, desktop, phone) asks for an explicit "continue without a password" before creating such an account.
> An unprotected `identity.key` can be read by anyone with file access. The storage key sits in it — meaning the entire database becomes readable.

Changing the passphrase also **replaces the storage key and re-encrypts the whole database** with the new one, in a single bbolt transaction. An old copy of `identity.key` opened with the old passphrase then yields a key that no longer opens the current database. The database holds a sealed check value, so a wrong key is reported as such instead of as corrupted records. Order of steps: the new key file is written next to the old one as `identity.key.new`, the database is re-encrypted, the new file replaces the old. If the app dies in between, the next unlock looks at which key the database already uses and either finishes the change (new passphrase) or rolls it back (old passphrase), telling you which one to type. The old passphrase is verified first, on purpose: without that check a typo would lock the account out permanently.

Attachments opened via the "Open" button land **decrypted** in `tmp/` inside the account's data directory (`0700`), wiped when the app closes and again when it starts. While it exists, the copy is plaintext.

---

## 🔒 DPI-Proof Traffic Obfuscation

Every peer-to-peer UDP datagram is wrapped in a ChaCha20 keystream:

| Detail | Value |
|---|---|
| **Key** | 32-byte random, generated by offerer, embedded in the Ed25519-signed SDP (`sigPayload.Obf`), sealed to the addressee |
| **Wire format** | `nonce(12B) \|\| ChaCha20(len(2B) \|\| plaintext \|\| random_padding(0–64B))` |
| **Nonce** | 12-byte random per datagram; safe to ~2⁴⁸ packets/key before birthday risk (session rotates every `Connect()`) |
| **Authentication** | None at this layer (unauthenticated cipher). DTLS underneath authenticates — tampered datagram decrypts to garbage DTLS drops. |
| **STUN bypass** | STUN/TURN server endpoints (discovered via `ResolveUDPAddr`) are stored in a bypass set and pass through unobfuscated. |
| **Relay bypass** | The peer's TURN relay candidates (from its SDP) also pass in the clear: a relay wraps what it forwards in TURN framing, so the relayed side never sees obfuscated bytes. To DPI that path looks like any WebRTC call through a TURN server. |
| **Connector TLS** | Client and mesh `wss://` dials send Chrome's ClientHello (uTLS) with ALPN `http/1.1`, as Chrome does for a WebSocket, instead of Go's recognisable `crypto/tls` fingerprint. |
| **mDNS** | Disabled (`MulticastDNSModeDisabled`) — no cleartext `.local` multicast alongside obfuscated flow. |

---

## 🛰️ Relay (TURN) and IP privacy

Peer-to-peer means peers see each other's ICE candidates: public and local IP. For an activist that is a location leak to every contact and every group member.

| Detail | Value |
|---|---|
| **Server** | Every connector with a known public address runs pion TURN + STUN on UDP at its listen port (`--turn auto`; `off`; or the public IP). Clients use it instead of Google's STUN; Google is used only when the connector offers nothing and no STUN is configured. |
| **Credentials** | TURN REST style (`expiry:id`, HMAC with a per-process secret), 24 h, handed out only in the `ok` reply to a signed `register`, refreshed over the WebSocket (`ice`). |
| **Bandwidth** | `--turn-rate` (KB/s, default 2048) caps relayed traffic of all users together, each way. Over the cap datagrams are dropped, not queued, so a connector on a home router keeps its owner's uplink. `0` = no cap. |
| **Abuse limits** | At most 64 live allocations per client key. Peers must be public addresses (`PermissionHandler`), so the relay is no door into the connector's own network. |
| **Relay-only mode** | `relay_only` / "Hide my IP address": `ICETransportPolicy = relay`, so only relay candidates are gathered and contacts learn the connector's IP, never yours. Without a TURN-capable connector no connection comes up — by design, no silent fallback that leaks. |
| **What the relay sees** | Who talks to whom, when, for how long and how much, and it can drop or block the link. It cannot read content: the DTLS fingerprint sits in the signed, sealed offer, so the relay cannot put itself in the middle, and messages are additionally encrypted with the epoch key. A recording of relayed traffic is noise to it. Anyone can run a connector, so an unknown operator must be assumed hostile for this metadata. |
| **Warnings** | Turning relay-only on needs an explicit "I understand" after a list of exactly that. Both ends detect a relayed link from the selected ICE pair (`Peer.Relay()`), so the other person is told too, even when relaying was not their choice; the chat header carries a "via relay" marker. |

---

## 📞 Calls

![Szept — Call Audio Pipeline](assets/call_audio_pipeline.svg)

- Voice is raw PCM (16 kHz mono, 20 ms frames); camera and screen share are JPEG frames. Everything rides the **same DTLS mesh** as messages — announced only to members already online, never relayed by a connector.
- The mixer's per-person gain, pan and mute, the master volume and deafen, and the 3D scene positions are **entirely local**. Nothing about how you arranged the room is transmitted; muting someone is your setting, not a message to them.
- Full mesh means every participant sees every stream — fine for a handful of people, and the reason there is no large-group mode.

---

## ⚖️ Known Trade-offs

| Area | Current state | Upgrade path |
|---|---|---|
| **Forward secrecy** | Sessions: yes (ephemeral DTLS). New members: groups are created without history by default, so a newcomer cannot read what was written before they joined (see Epoch keys). What stays: every member's device stores the history it has read and the keys for it, so a seized device or a hostile member still exposes everything that member saw. A Double Ratchet would not change that here: events are kept and replicated as a signed DAG, so their keys must outlive the session, and ciphertext on the wire is already under ephemeral DTLS. | Disappearing messages (local expiry of old events and their keys), once the DAG can prune history it no longer needs for authorization. |
| **Offline delivery** | None — P2P means no mailbox. | Opt-in sealed-sender store-and-forward relay. |
| **Connector metadata** | Sees connection graph (like a torrent tracker). | Self-host + Tor/VPN + Cebulion mesh. |
| **Rendezvous dependency** | With no reachable connector you cannot open a new link or rebuild a dropped one — mDNS is off and there is no direct-IP dial. Established links and local writes are unaffected. | A Cebulion mesh with more than one node; LAN discovery would drop the dependency for peers on the same network. |
| **Webhook token** | Kept in the encrypted database, shown in Settings. A local process can still use the webhook while the app runs if it learns the token (from you, or by reading the app's memory). | A per-caller token with a scope. |
| **Connector lists** | Fetching one reveals your IP to its host, and its entries land in your switch list. | Pin the connectors you trust and skip lists entirely. |
| **Profile card** | Status, description and banner are visible to everyone sharing any chat with you. | Per-chat visibility, if it ever proves worth the complexity. |
| **Presence oracle** | The connector answers `lookup` for **any** pubkey — it knows no relationships, so it cannot tell who is entitled to ask about whom. Pubkeys are public (friend codes), so this is a presence oracle for the whole network, rate-limited to 60/min per session. Since lookups also go out to the mesh, a hostile mesh node sees which keys are being looked for (not who asks). | Blinded presence, or accepting that a rendezvous server knows who is online. |
| **No passphrase** | Still allowed, now behind an explicit confirmation and a warning in the CLI, desktop and phone setup screens. Without one, `identity.key` and the whole database sit on disk unencrypted. | Require one, at the cost of locking out users who lose it. |
| **Contact graph via presence** | By default the client asks its connector every 30 s whether each contact and group member is online, so the connector learns most of your contact list, even people you never message. **"Hide my contact list from the connector"** (`quiet_lookup`) limits lookups to members of chats you opened, typed or sent something in during the last 15 minutes; everyone else shows a stale presence dot until they connect to you. Even then the connector sees who you actually talk to, because it carries the signaling. Blinded presence would not change that: whoever routes the offer knows both ends. | A connector you or someone you trust runs, reached over Tor or a VPN. |
| **Mesh routing** | A signal goes to the neighbour that answered `mesh_found`, plus up to 4 others, with TTL 5, then once more to every neighbour if no receipt comes back. Past TTL 5 a signal can still miss in a very large mesh; the sender is now told (`undelivered`) and retries on the next round. Bloom filters of hosted keys in gossip would route better, but were rejected: they would let anyone test which connector hosts a given key, which locates a user. | Retrying through a second saved connector when the first reports `undelivered`. |
| **Distribution (Chat Control)** | Client-side scanning laws act on app stores and service providers. Szept has no provider, so the weak point is the install channel: a store can force changes or pull the app. `make release` builds the connector for servers, Raspberry Pi and OpenWrt routers (amd64, arm, arm64, mips, mipsle, Windows, macOS) reproducibly (`-trimpath`, no VCS stamp, empty build ID; same commit + same Go version = same bytes) and signs `SHA256SUMS` with an SSH key (`ssh-keygen -Y sign`). `make android KEYSTORE=… KS_PASS=… KS_ALIAS=…` signs the APK with a release key instead of the debug one. Clients use cgo, so their hashes only match with the same C toolchain. | Publish the signing key's fingerprint in several independent places; F-Droid build metadata for the APK. |
| **Mesh calls** | Each participant sees all streams (full mesh). | SFU relay for larger groups. |
| **Traffic analysis** | Packet size/timing can reveal activity. | DTLS-level variable padding (Cebulion already adds 0–64B random pad per datagram). |
| **Orphan flooding** | Capped at 5000 total orphaned events across all chats. | Per-peer rate limiting. |
| **Blob assembly memory** | Capped at 64 MiB total across all concurrent transfers. | Disk-backed assembly for large files. |
