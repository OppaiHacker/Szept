# 🔒 Szept — Unstoppable Peer-to-Peer Encrypted Messenger

> **Zero Central Servers. Total Privacy. Unstoppable P2P Networking.**
> 
> *Szept* (Polish for *Whisper*) is a cutting-edge, peer-to-peer, end-to-end encrypted communication platform built in Go. Available as both a lightweight terminal interface (**TUI**) and a modern cross-platform graphical application (**GUI**) for Linux, Windows, and Android.

---

## ✨ Features at a Glance

- **🔒 True End-to-End Encryption (E2EE)**: Cryptographic identity backed by **Ed25519** signatures and **XChaCha20-Poly1305** epoch keys.
- **⚡ Peer-to-Peer & Serverless Content**: No central storage or database of your conversations. Content is exchanged directly between peers over **WebRTC**.
- **🌐 Cebulion Traffic Obfuscation (DPI-Proof)**: Peer datagrams are encrypted into pseudo-random streams via **ChaCha20**. Removes STUN/DTLS headers to bypass Deep Packet Inspection (DPI) and firewalls.
- **📜 Replicated DAG Ledger ("Blockchain-like")**: Every chat is a signed, hash-linked Directed Acyclic Graph (DAG). History syncs automatically whenever peers connect.
- **📞 Mesh Voice, Video & Screen Share**: Real-time multi-user voice calls, camera streaming, and desktop sharing (up to ~8 peers mesh).
- **📁 Smart File & Media Attachments**: Inline previews for images, GIFs, and videos. Files over 10 MB replicate initial thumbnails, pulling full payload on-demand.
- **💬 Modern Chat Features**:
  - **Typing Indicators**: Real-time, zero-storage ephemeral typing cues.
  - **Delivery & Read Receipts (ACKs)**: Instant delivery (`✓`) and read (`✓✓`) tracking.
  - **Emoji Reactions**: Interactive emoji reactions (`KindReact`) attached directly to DAG events.
  - **Local History Search**: Instant full-text search across local encrypted history.
  - **Global & Soft Message Deletion**: True global deletion event (`KindDelete`) across the DAG + local "Hide for me" soft-delete.

---

## 🏗️ Architecture & Connector Overview

*Szept* requires no central application servers. The only infrastructure component is a lightweight **Connector tracker** (similar to a torrent tracker). The connector **never** sees message contents or keys — it only helps peers discover each other's network addresses.

![Szept Connector Architecture](assets/connector_architecture.jpg)

### Key Cryptographic Principles:
1. **Genesis & Keys**: Creating a chat generates an epoch key. Members are invited using one-time signed codes containing the inviter's public key (preventing MITM).
2. **DAG Synchronization**: Events are hash-linked with Lamport counters. Upon connecting, peers exchange head hashes and request missing event deltas.

---

## 🧅 Cebulion Network & DPI Obfuscation

To guarantee connectivity under strict censorship, firewall filtering, or DPI (Deep Packet Inspection), *Szept* implements **Cebulion datagram obfuscation**.

![Cebulion Obfuscation Network](assets/cebulion_obfuscation.jpg)

- **Random Stream Wrapping**: Peer-to-peer datagrams are wrapped in a ChaCha20 keystream initialized via signed offer tokens.
- **Header Concealment**: Eliminates visible WebRTC, DTLS, or STUN packet headers. On the wire, traffic appears as unidentifiable high-entropy noise.
- **Variable Padding**: Randomizes packet lengths to thwart side-channel packet-length fingerprinting.

---

## 🚀 Quickstart

### 1. Build from Source
Requirements: Go ≥ 1.22, gcc, X11/GL headers (Linux GUI).

```bash
make          # Builds bin/szept-connector, bin/szept (TUI), bin/szept-gui
make test     # Runs test suite
```

### 2. Run the Connector (VPS / Public Server)
```bash
./bin/szept-connector --listen :7777
# TLS optional: --cert cert.pem --key key.pem (clients connect via wss://)
```

### 3. Launch Client & Add Friends
1. Start `./bin/szept` (TUI) or `./bin/szept-gui` (GUI).
2. Share your ID code via `/mojkod` (TUI) or **Friends → Your Code** (GUI).
3. Add a friend using `/znajomy <code_or_qr>`.
4. Create a group chat with `/nowy <group_name>` and invite friends using `/zapros`.

---

## ⌨️ Commands Quick Reference

| Command | Description |
| :--- | :--- |
| `/nowy <name>` | Create a new group chat |
| `/dm <friend>` | Open 1:1 chat with a friend |
| `/zapros` | Generate one-time invite QR & PIN |
| `/dolacz <code> <pin>` | Join a group chat using invite code |
| `/szukaj <query>` | Search encrypted history |
| `/r <msg_no> <emoji>` | React to a message with emoji |
| `/usun <msg_no>` | Delete message for everyone (Global DAG delete) |
| `/ukryj <msg_no>` | Hide message locally (Soft delete) |
| `/call`, `/rozlacz` | Join / leave voice call |
| `/mic`, `/cam`, `/ekran` | Toggle mic, camera, screen share |

---
---

# 🇵🇱 Szept — Niezrównany Komunikator Peer-to-Peer z Pełnym Szyfrowaniem

> **Zero Centralnych Serwerów. Całkowita Prywatność. Komunikacja Nie do Zablokowania.**
> 
> *Szept* to nowoczesny, odporny na cenzurę komunikator peer-to-peer z szyfrowaniem end-to-end napisany w języku Go. Dostępny w wersji konsolowej (**TUI**) oraz graficznej (**GUI**) dla systemów Linux, Windows i Android.

---

## ✨ Przegląd Możliwości

- **🔒 Pełne Szyfrowanie End-to-End (E2EE)**: Tożsamość oparta o podpisy **Ed25519** oraz klucze epokowe **XChaCha20-Poly1305**.
- **⚡ Zdecentralizowana Architektura P2P**: Treści rozmów przesyłane bezpośrednio między urządzeniami po **WebRTC**. Brak jakiejkolwiek bazy danych na serwerach!
- **🌐 Obfuskacja Ruchu Cebulion (Odporność na DPI)**: Datagramy peer-to-peer są szyfrowane strumieniem **ChaCha20**. Ukrywają nagłówki STUN/DTLS, uniemożliwiając rozpoznanie i zablokowanie ruchu przez zapory firewall i analizatory DPI.
- **📜 Replikowany DAG ("Blockchain dla Wiadomości")**: Każdy czat to cyfrowo podpisany, powiązany hashami skierowany graf acykliczny (DAG). Historia synchronizuje się automatycznie przy każdym połączeniu.
- **📞 Głos, Kamera i Ekran w Sieci Mesh**: Rozmowy głosowe, przekaz z kamery oraz udostępnianie pulpitu w grupach (do ~8 osób).
- **💬 Nowoczesne Funkcje Komunikatora**:
  - **Wskaźniki pisania (Typing)**: Informacja *"Ania pisze..."* na żywo (bez obciążania bazy).
  - **Potwierdzenia dostarczenia i odczytu (ACKs)**: Znaczniki `✓` (dostarczono) i `✓✓` (przeczytano).
  - **Reakcje Emoji**: Interaktywne emoji dodawane jako zdarzenia `KindReact` w DAG-u.
  - **Lokalne Wyszukiwanie**: Przeszukiwanie całej zaszyfrowanej historii czatu.
  - **Trwałe i Lokalne Usuwanie**: Globalne usunięcie wiadomości u wszystkich (`KindDelete`) oraz lokalne ukrywanie („Ukryj u mnie”).

---

## 🏗️ Architektura i Connector

*Szept* nie wymaga centralnych serwerów treści. Jedynym elementem infrastruktury jest lekki **Connector** (odpowiednik trackera w torrentach), który pomaga peerom odnaleźć się w sieci. Connector **nigdy** nie ma dostępu do kluczy ani treści wiadomości.

![Architektura Connectora Szept](assets/connector_architecture.jpg)

---

## 🧅 Obfuskacja Ruchu i Sieć Cebulion

Aby zapewnić niezawodną łączność w warunkach cenzury sieciowej lub blokad krajowych, *Szept* szyfruje cały ruch P2P w sieci **Cebulion**.

![Obfuskacja Ruchu Cebulion](assets/cebulion_obfuscation.jpg)

Pakiety na łączu wyglądają jak całkowicie losowy szum o wysokiej entropii i zmiennej długości — systemy DPI nie są w stanie rozpoznać sygnatur WebRTC ani zablokować transmisji.

---

## 📄 Licencja

[GNU General Public License v3.0](LICENSE) (GPL-3.0-or-later).
