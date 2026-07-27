# 🔒 Szept — Peer-to-Peer Encrypted Messenger

> **No servers. No phone numbers. No metadata leaks.**
>
> *Szept* (Polish for *whisper*) is a peer-to-peer, end-to-end encrypted messenger written in Go.
> Available as a terminal client (**TUI**), a desktop GUI (**Linux / Windows**), and a mobile app (**Android**).

---

## ✨ Features

| Category | What you get |
|---|---|
| **Encryption** | Ed25519 identity · XChaCha20-Poly1305 epoch keys · DTLS 1.2+ transport |
| **P2P & Serverless** | Content never touches a server. Peers exchange data directly over WebRTC. |
| **Cebulion Network** | Distributed mesh of connectors with SNDP gossip discovery, Dijkstra-routed signaling, and automatic failover — if any connector goes down, the network keeps running. |
| **DPI-Proof Obfuscation** | Every peer-to-peer UDP datagram is wrapped in a ChaCha20 keystream with variable random padding — STUN/DTLS headers vanish, leaving only high-entropy noise. |
| **DAG History** | Every chat is a signed, SHA-256 hash-linked DAG with Lamport-ordered events. History syncs automatically when peers reconnect. |
| **Voice, Video & Screen** | Real-time mesh calls with camera and desktop sharing (up to ~8 peers). |
| **File Attachments** | Inline previews for images, GIFs, video. Large files (>10 MB) replicate a thumbnail first; full payload pulled on demand in 16 KB chunks. |
| **Modern Chat UX** | Typing indicators · delivery & read receipts (`✓` / `✓✓`) · emoji reactions · full-text search · global & local message deletion |

---

## 🏗️ Architecture & Connection Flow

Szept requires **no central application server**. The only infrastructure is a lightweight **Connector** — a ~200-line stateless relay (like a torrent tracker). It maps `pubkey → socket` and relays signed signaling blobs. It **never** sees message content, keys, or membership.

![Szept — Connector Architecture & Connection Flow](assets/connector_architecture.jpg)

**Connection lifecycle:**
1. **Register** — Client sends Ed25519-signed timestamp; connector maps pubkey to WebSocket.
2. **Signal** — SDP offer (containing DTLS cert fingerprint + 32-byte obfuscation key) is Ed25519-signed and relayed. ±2 min freshness window, explicit addressee, glare tie-break (lower pubkey wins).
3. **Connect** — DTLS 1.2+ handshake directly between peers. Certificate is identity-bound — connector cannot MITM.
4. **Sync** — Peers exchange DAG head hashes and pull missing events over the authenticated E2E channel.

---

## 🧅 Cebulion Network — Resilient Connector Mesh

A single connector works fine, but connectors can also join the **Cebulion Network** — a distributed mesh that provides fault tolerance. If any connector goes down or gets blocked, the rest of the network keeps routing signals.

![Szept — Cebulion Network Mesh](assets/cebulion_network.jpg)

### How it works

| Component | Mechanism |
|---|---|
| **SNDP Gossip** | Every 15 seconds, each connector pings its seeds and known neighbors via `/ndp/neighbors`, exchanging peer counts, RTT latencies, and the full topology graph. |
| **Topology Map** | A BGP-like link-state graph: `nodeA → nodeB → RTT(ms)`. Max 256 nodes, max 64 neighbors per connector. |
| **Dijkstra Routing** | When a signal's destination isn't local, the connector computes the shortest path (RTT-weighted) and forwards `mesh_signal` to the optimal next hop. |
| **Loop Prevention** | Each `mesh_signal` carries a random `TraceID` and a `TTL` (default 5). Seen traces are deduplicated for 5 minutes. |
| **Load Balancing** | When `--max-peers` is reached, the connector upgrades the WebSocket, sends a `redirect` with active neighbor URLs, and closes — the client reconnects to a neighbor automatically. |
| **Fallback URLs** | Clients maintain a list of `FallbackURLs` (populated from redirects and invite codes); on disconnect, they try each URL before backing off. |
| **DNS Resilience** | If system DNS fails (`no such host`), the client falls back to resolving via `1.1.1.1:53`, and if port 53 is blocked, via DNS-over-HTTPS (DoH) to `1.1.1.1`. |

**Starting a connector in the mesh:**
```bash
./bin/szept-connector --listen :7777 \
  --public-url ws://c1.example.com:7777 \
  --seeds ws://c2.example.com:7777,ws://c3.example.com:7777
```

---

## 🔒 DPI-Proof Traffic Obfuscation

Under strict firewalls or deep packet inspection, Szept wraps all peer-to-peer UDP datagrams in a **ChaCha20 obfuscation layer**.

![Szept — Cebulion Traffic Obfuscation](assets/cebulion_obfuscation.jpg)

- **Per-connection key** — the offerer generates a 32-byte ChaCha20 key and embeds it in the Ed25519-signed SDP offer (`sigPayload.Obf`). Only the addressed peer can derive the keystream.
- **Wire format** — `nonce(12B) || ChaCha20(len(2B) || plaintext || random_padding(0–64B))`. No MAC — DTLS underneath already authenticates, so a tampered datagram decrypts to garbage that DTLS drops.
- **STUN bypass** — traffic to public STUN servers (discovered via `ResolveUDPAddr`) passes through unobfuscated so ICE server-reflexive candidates still work.
- **mDNS disabled** — prevents `.local` multicast from leaking cleartext alongside the obfuscated flow.

**Result**: on the wire, traffic is indistinguishable from random noise — DPI cannot fingerprint WebRTC, STUN, or DTLS protocols.

---

## 🚀 Quickstart

### Build from source

Requirements: **Go ≥ 1.22**, gcc, X11/GL headers (Linux GUI).

```bash
make          # builds bin/szept-connector, bin/szept (TUI), bin/szept-gui
make test     # runs the test suite
```

### Run a standalone connector
```bash
./bin/szept-connector --listen :7777
# TLS: --cert cert.pem --key key.pem → clients connect via wss://
```

### Join the Cebulion mesh
```bash
./bin/szept-connector --listen :7777 \
  --public-url ws://your-vps.example.com:7777 \
  --seeds ws://seed1.example.com:7777
```

### Launch & add friends

1. Start `./bin/szept` (TUI) or `./bin/szept-gui` (GUI).
2. Share your ID — `/mycode` in TUI or **Friends → Your Code** in GUI.
3. Add a friend — `/friend <code>`.
4. Create a group — `/new <name>`, then invite with `/invite`.

---

## ⌨️ Commands (English / Polish aliases)

| Command | Description |
|---|---|
| `/new <name>` · `/nowy` | Create group chat |
| `/dm <friend>` | Open 1:1 chat |
| `/invite` · `/zapros` | Generate one-time invite (QR + PIN) |
| `/join <code> <pin>` · `/dolacz` | Join group from invite |
| `/friend <code>` · `/znajomy` | Add friend |
| `/search <q>` · `/szukaj` | Search encrypted history |
| `/react <n> <emoji>` · `/r` | Emoji reaction |
| `/delete <n>` · `/usun` | Delete for everyone (DAG event) |
| `/hide <n>` · `/ukryj` | Hide locally |
| `/send <path>` · `/wyslij` | Send file |
| `/save <n>` · `/zapisz` | Save attachment to disk |
| `/avatar <path>` · `/awatar` | Set profile picture |
| `/mycode` · `/mojkod` | Show your friend code |
| `/who` · `/kto` | List chat members |
| `/rename <name>` · `/zmien` | Rename chat |
| `/rekey` | Manual epoch key rotation |
| `/call` · `/leave` · `/rozlacz` | Join / leave voice call |
| `/mic` · `/cam` · `/screen` · `/ekran` | Toggle mic / camera / screen share |
| `/help` · `/pomoc` | Command list |

---
---

# 🇵🇱 Szept — Komunikator Peer-to-Peer z Szyfrowaniem End-to-End

> **Brak centralnych serwerów. Całkowita prywatność. Komunikacja odporna na cenzurę.**
>
> *Szept* to komunikator peer-to-peer z pełnym szyfrowaniem end-to-end, napisany w Go. Dostępny w wersji konsolowej (**TUI**), graficznej (**GUI**) na Linuksa i Windowsa, oraz mobilnej na **Androida**.

---

## ✨ Przegląd

| Kategoria | Co dostajesz |
|---|---|
| **Szyfrowanie** | Tożsamość Ed25519 · klucze epokowe XChaCha20-Poly1305 · transport DTLS 1.2+ |
| **P2P** | Treści nigdy nie trafiają na serwer — wymiana bezpośrednia po WebRTC. |
| **Sieć Cebulion** | Rozproszona siatka connectorów z gossipem SNDP, routingiem Dijkstry i automatycznym failoverem — padnięcie dowolnego connectora nie zatrzymuje komunikatora. |
| **Obfuskacja DPI** | Każdy datagram UDP owinięty ChaCha20 ze zmiennym paddingiem — nagłówki STUN/DTLS znikają, ruch wygląda jak losowy szum. |
| **DAG** | Każdy czat to podpisany, powiązany hashami SHA-256 graf acykliczny z porządkiem Lamporta. Historia synchronizuje się automatycznie. |
| **Głos i wideo** | Rozmowy głosowe, kamera i udostępnianie ekranu w meshu (do ~8 osób). |
| **Pliki** | Podgląd inline dla grafik, GIF-ów i wideo. Duże pliki ładowane na żądanie fragmentami po 16 KB. |
| **UX** | Wskaźniki pisania · potwierdzenia dostarczenia (`✓` / `✓✓`) · reakcje emoji · wyszukiwanie · usuwanie globalne i lokalne |

---

## 🏗️ Architektura

Jedynym elementem infrastruktury jest **Connector** — ~200-liniowy, stateless relay. Mapuje `pubkey → gniazdo` i przekazuje podpisane SDP. **Nigdy** nie widzi treści, kluczy ani członkostwa.

![Szept — Architektura Connectora](assets/connector_architecture.jpg)

---

## 🧅 Sieć Cebulion — Rozproszona Siatka Connectorów

Connector może działać sam lub dołączyć do **Sieci Cebulion** — rozproszonej siatki connectorów. Jeśli jakiś connector padnie lub zostanie zdjęty, reszta sieci przejmuje routing sygnałów. Klient automatycznie przełącza się na żywe węzły.

![Szept — Sieć Cebulion](assets/cebulion_network.jpg)

**Jak działa:**
- **SNDP Gossip** — connektory co 15 sekund wymieniają listę sąsiadów i topologię (RTT)
- **Dijkstra Routing** — sygnał trafia do celu najszybszą ścieżką przez siatkę
- **Failover** — padnięcie węzła = automatyczny rerouting; klient próbuje fallback URL-i
- **Load Balancing** — przeciążony connector odsyła klienta do sąsiada z wolnymi slotami

---

## 🔒 Obfuskacja Ruchu

Pod restrykcyjnymi firewallami *Szept* owija cały ruch P2P w warstwę obfuskacji ChaCha20. Pakiety na łączu wyglądają jak losowy szum o zmiennej długości — systemy DPI nie są w stanie rozpoznać sygnatur WebRTC.

![Szept — Obfuskacja Ruchu](assets/cebulion_obfuscation.jpg)

---

## ⌨️ Komendy (polskie / angielskie aliasy)

| Komenda | Opis |
|---|---|
| `/nowy <nazwa>` · `/new` | Utwórz grupę |
| `/dm <znajomy>` | Otwórz czat 1:1 |
| `/zapros` · `/invite` | Generuj jednorazowe zaproszenie (QR + PIN) |
| `/dolacz <kod>` · `/join` | Dołącz do grupy |
| `/znajomy <kod>` · `/friend` | Dodaj znajomego |
| `/szukaj <fraza>` · `/search` | Szukaj w historii |
| `/r <nr> <emoji>` · `/react` | Reakcja emoji |
| `/usun <nr>` · `/delete` | Usuń u wszystkich |
| `/ukryj <nr>` · `/hide` | Ukryj lokalnie |
| `/wyslij <ścieżka>` · `/send` | Wyślij plik |
| `/zapisz <nr>` · `/save` | Zapisz załącznik |
| `/awatar <ścieżka>` · `/avatar` | Ustaw awatar |
| `/mojkod` · `/mycode` | Twój kod znajomego |
| `/kto` · `/who` | Członkowie czatu |
| `/zmien <nazwa>` · `/rename` | Zmień nazwę czatu |
| `/rekey` | Ręczna rotacja klucza epoki |
| `/call` · `/rozlacz` | Dołącz / wyjdź z rozmowy |
| `/mic` · `/cam` · `/ekran` | Mikrofon / kamera / ekran |
| `/pomoc` · `/help` | Lista komend |

---

## 📄 License

[GNU General Public License v3.0](LICENSE) (GPL-3.0-or-later).
