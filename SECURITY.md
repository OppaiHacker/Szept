# Model bezpieczeństwa

## Tożsamość

- Para kluczy **Ed25519** = tożsamość. Plik klucza szyfrowany
  **argon2id** (3 iteracje, 64 MiB) + **XChaCha20-Poly1305**, uprawnienia 0600.
- Odcisk (fingerprint) pokazywany w UI; kody znajomego/zaproszenia **zawierają
  pełny klucz publiczny**, więc dodanie kontaktu nie opiera się na zaufaniu do
  serwera ani na TOFU — MITM przy nawiązaniu kontaktu wymagałby podmiany kodu
  poza aplikacją.

## Transport (P2P)

- Połączenia: WebRTC **DTLS 1.2+** (pion), NAT traversal przez ICE/STUN.
- Sygnaling (SDP z odciskiem certyfikatu DTLS) przechodzi przez connector, ale
  jest **podpisany Ed25519 nadawcy i weryfikowany względem oczekiwanego klucza
  odbiorcy** (+ świeżość ±2 min, adresat, tie-break kolizji). Connector **nie
  może** podmienić ofert ani zrobić MITM — DTLS jest związany z tożsamością.
- Efekt: każdy kanał danych to uwierzytelniona rura E2E między dwiema znanymi
  tożsamościami. Nie ma TURN, więc nie istnieje żaden pośrednik treści.

## Treści czatów

- Czat = **DAG zdarzeń** (rodzice = hashe SHA-256), każde zdarzenie:
  - podpisane Ed25519 autora (podpis obejmuje ciphertext i całą strukturę),
  - ciało zaszyfrowane **XChaCha20-Poly1305** kluczem **epoki** czatu,
    z kontekstem (chat, autor, rodzice, epoka) jako associated data.
- Replikacja: wymiana headów + dociąganie brakujących przodków; deduplikacja po
  ID; zdarzenia weryfikowane **przed** zapisem (podpis + autoryzacja przez fold:
  genesis→właściciel, add→członek, remove/rekey→właściciel, msg→członek).
  Historia jest niezmienialna — modyfikacja zmienia hash i unieważnia potomków.
- **Klucze epok** wędrują wyłącznie kanałami E2E (DTLS) do zweryfikowanych
  członków; nigdy nie są nadpisywane; zły klucz = wykrywalny błąd AEAD.
- **Usunięcie członka** → automatyczna rotacja epoki: wyrzucony nie odszyfruje
  nowych wiadomości i nie dostaje nowych zdarzeń (peers odmawiają synca).
  Ręczna rotacja: `/rekey`.
- Zaproszenia: sekret 128-bit, po stronie zapraszającego trzymany **hash**;
  jednorazowość egzekwowana atomowo.

## Załączniki i awatary

- W DAG-u leży **wyłącznie metryczka**: `sha256` treści, typ MIME, nazwa,
  rozmiar, wymiary — całość w zaszyfrowanym ciele podpisanego zdarzenia.
  Same bajty jadą osobno, na żądanie, kawałkami po tym samym kanale E2E.
- Odbiorca **liczy skrót** przyjętej treści i porównuje z tym, co obiecywało
  podpisane zdarzenie → podmiana pliku po drodze jest wykrywalna. Skrót znany
  jest tylko z odszyfrowanej historii, więc działa jak przepustka: kto go nie
  ma, nie ma jak poprosić o plik. Serwujemy wyłącznie członkom danego czatu.
- **Typ pliku wykrywamy z magicznych bajtów** (`http.DetectContentType`), nigdy
  z nazwy ani z pola przysłanego przez drugą stronę. Dozwolone: PNG, JPEG, GIF,
  WebP, MP4, WebM — reszta (HTML, SVG, skrypty, archiwa, ELF) odpada.
- **Nazwa pliku jest generowana u nas**: obcinana do samej nazwy (bez ścieżek),
  bez znaków sterujących, z rozszerzeniem wymuszonym przez wykryty typ. Plik z
  sieci nie ma jak wylądować na dysku jako `.sh` czy `.exe`.
- **Awatar jest przekodowywany**: dekodujemy go w całości, przycinamy do
  kwadratu 256 px i kodujemy od nowa (PNG, a GIF pozostaje animowanym GIF-em).
  Do bazy trafiają bajty z *naszego* enkodera, więc doklejony ładunek (polyglot
  ZIP/HTML/JS, EXIF, komentarze, profile ICC) po prostu nie przeżywa operacji.
- Zapory na bomby dekompresyjne: limit bajtów (32 MiB załącznik, 4 MiB wejście
  awatara), limit wymiarów (8000 px na bok, 20 MPx) i liczby klatek GIF-a,
  sprawdzane **przed** pełnym dekodowaniem. Wideo nie jest dekodowane — nie
  mamy dekodera i nie udajemy walidacji zawartości; chroni je to, że pliku
  nigdy nie uruchamiamy sami (oddajemy go systemowemu odtwarzaczowi).

## Dane na dysku

- Zdarzenia leżą w bazie tak, jak w sieci — **ciała zaszyfrowane**.
- Treść załączników i awatarów (bloby) szyfrowana kluczem magazynu, adresowana
  swoim skrótem — ta sama grafika w pięciu czatach leży na dysku raz.
- Metadane (nazwy czatów, klucze epok, znajomi, zaproszenia) szyfrowane kluczem
  wyprowadzonym (HKDF) z seeda tożsamości → bez odszyfrowania `identity.key`
  baza jest bezużyteczna. Hasło do klucza = szyfrowanie całości w spoczynku.

## Co widzi connector (uczciwie)

- pary **pubkey ↔ IP** zarejestrowanych klientów i to, **kto z kim** wymienia
  sygnaling (metadane połączeń). Nie widzi: treści, nazw, członkostwa, kluczy.
- Mitygacje: postaw **własny** connector (200 linii, stateless), używaj `wss://`,
  w razie potrzeby Tor/VPN. Rejestracja wymaga podpisu z timestampem — nie da
  się podszyć pod cudzy pubkey ani odtworzyć starej rejestracji.

## Znane kompromisy / ścieżki rozwoju

- **Forward secrecy**: sesje mają FS (efemeryczne DTLS), historia czatu nie —
  klucze epok są przechowywane, bo nowi członkowie mają czytać historię
  (świadoma decyzja produktowa). Częste `/rekey` zawęża okno; Double Ratchet
  (na czatach 1:1) to naturalny upgrade.
- **Brak offline delivery** — z definicji P2P; brak serwera = brak skrzynki.
- **Metadane u connectora** (jak tracker w torrentach) — patrz mitygacje wyżej.
- Wiadomości głosowe/wideo idą po DTLS między peerami; w grupie każdy uczestnik
  widzi strumienie pozostałych (mesh) — jak w każdym komunikatorze.
- Rozmiar/częstotliwość pakietów może zdradzać aktywność obserwatorowi sieci
  (padding to możliwy upgrade).
