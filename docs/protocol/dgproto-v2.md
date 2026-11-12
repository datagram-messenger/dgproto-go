---
title: "DGProto 2"
subtitle: "End-to-end encrypted messaging protocol — endpoint and relay specification"
kicker: "Protocol specification"
revision: "1"
compatibility: "DG2-P2 / B1-R2"
scope: "Verified single-device 1:1 messaging"
status: "Draft · frozen for implementation"
date: "2026-11-12"
notice: "Independent security review, a conformant implementation and interoperability testing are required before this protocol carries real user data."
running: "DGPROTO 2 · REVISION 1 · DG2-P2 / B1-R2"
footer: "DRAFT"
---

## Document status

| Property | Value |
|---|---|
| Revision | **1** (2026-11-12) |
| Compatibility ID | **DG2-P2 / B1-R2**: application version 2, cryptographic profile 2 |
| Status | Draft, frozen for implementation · not implemented · not audited (Sections 1.4 and 11.4) |
| Scope | One endpoint/relay protocol for verified single-device 1:1 messaging: bounded text, opaque bodies and encrypted receipts |
| Session design | One active bidirectional Double Ratchet per contact, with bounded receive-only overlap and monotone generations |
| Carrier | Binding B1-R2: Noise/TCP encapsulation on a dedicated service (Section 10) |
| Normative content | Sections 1–12; Section 13 records decisions and references |

> [!NOTE]
> **Frozen for implementation**
> The design and wire format are fixed for building the reference implementation. Defects found while implementing are corrected in place as numbered revisions (Section 11.4). The document becomes Stable once the Section 12 gates are met.

# 1. Authority, purpose and scope

## 1.1. Normative authority

The key words MUST, MUST NOT, SHOULD, SHOULD NOT and MAY are to be interpreted as described in RFC 2119 and RFC 8174. A departure from a SHOULD requires a recorded reason and an analysis of its consequences. [1] [2]

Sections 1–12 define the contract of this edition. Within them, exact grammars, bounds, state transitions and required outcomes are normative even where no capitalized key word appears. The external algorithms explicitly incorporated by Sections 3 and 10 are normative within the stated bounds. Section 13 records decisions, sources and evidence; it cannot override a rule. Non-normative commentary and supplemental review material add no requirements.

An edition is identified by its title, revision and compatibility ID. A publication MUST identify the revision, the compatibility ID and the revisions of all incorporated external specifications. Its release record MUST identify the exact issued artifact by SHA-256; that hash is kept outside the artifact itself. Other formats of the same edition MUST preserve the normative content. Any change to the contract requires a controlled revision under Section 11.4.

## 1.2. One protocol and two roles

DGProto defines the contract between endpoints and an authoritative relay for Datagram. It provides asynchronous end-to-end encrypted (E2EE) delivery with explicit durable state: verified trust, bounded setup, immutable ciphertext retries, replay control, honest acknowledgement stages and crash-consistent effects.

| Role | Owns |
|---|---|
| Endpoint | Identity trust, E2EE state, inbox/outbox, application-effect admission |
| Relay | Current account authorization, prekey reservations, opaque mailboxes, operation journals, service admission |

These are two roles of one protocol, not separate platform-specific specifications. Every port MUST meet the same rules over a supported binding. Browser raw TCP is not assumed to be available.

Binding B1-R2 is the only binding specified, and no second binding is implicitly supported. Core E2EE bytes contain no TCP or Noise sequence numbers and MUST survive a future, separately specified binding without reencryption. WSS, QUIC, browser authentication, federation, relay migration and transport fallback are not defined.

Profile 1 and binding B1-R1 are reserved identifiers and are never valid (Section 11.4). There is no downgrade, trial decryption, compatibility negotiation or plaintext fallback.

## 1.3. Fixed product boundary

Profile 2 permits exactly one trusted device per local identity and at most 100 verified contacts. It carries asynchronous one-to-one UTF-8 text, bounded opaque bytes and encrypted receipts. Both parties MUST independently verify trust before setup. Routine session replacement preserves identity keys.

Delivery is not lossless across every offline interval. Mailboxes, keys, replay windows and overlap are finite, and old unconfirmed ciphertext is never silently reencrypted.

Out of scope: groups, multiple concurrent devices, attachments and chunking, backup and history transfer, automatic identity recovery, sealed sender, anonymity, key transparency, federation, presence, typing events and post-quantum protection. Implementations MUST NOT accept or advertise any of these as an extension of profile 2. Opaque bytes do not authorize unsafe object deserialization or remote execution.

## 1.4. Specification, conformance and readiness

| Claim | Meaning in this edition | Current result |
|---|---|---|
| Specification baseline | One selected grammar, state machine, failure policy and compatibility boundary | Revision 1, frozen for implementation; corrections follow Section 11.4 |
| Implementation conformance | An implementation satisfies all applicable normative rules and evidence gates | Not established; no endpoint/relay implementation supplied |
| Operational/security readiness | Reviewed composition and code, tested storage/platform model, remediated findings and measured deployment | Not established; real user data prohibited until Section 12 gates |

No independent composition review, security audit, benchmark or interoperability result exists for this edition. Abstract models, codec checks and KDF cross-checks provide only limited regression evidence. Published analysis of the individual primitives is not an analysis of this composition. Reaching Stable status does not change this assurance boundary.

# 2. Notation, identities and trusted context

## 2.1. Exact encodings

- `||` concatenates bytes. `[a:b]` is a half-open slice.
- `u8/u16/u32/u64` are unsigned 1/2/4/8-byte integers. All multibyte application integers, including counters and lengths, are little-endian; `LE16`, `LE32` and `LE64` encode that representation.
- `bytesN` has exactly N bytes and no implicit prefix.
- ASCII labels have no NUL, newline, length prefix or implicit normalization.
- No varints, JSON, Protobuf, implicit optional fields or trailing bytes are used.
- All size limits count bytes, not characters. Count and byte ceilings are inclusive.
- Arithmetic MUST be checked for overflow.
- A deadline is exclusive: at now >= deadline, new use or admission is forbidden even if cleanup has not run. Controlling ages are rechecked at the successful commit. A late observation may reconcile an earlier commit but does not extend its original eligibility.

`raw(PK)` is the canonical 32-byte X25519 u-coordinate. `Encode(PK) = 0x01 || raw(PK)` is the 33-byte encoding called Key33. Before trust comparison or DH, a decoder MUST reject a discriminator other than 1, a top bit of 1, or u >= 2^255−19. It MUST NOT mask or reduce a noncanonical alias into a trusted key. Every DH MUST reject a provider failure and an all-zero result before KDF or commit. These application checks are stricter than the permissive primitive input decoding in RFC 7748; they do not change that primitive. [3]

A raw key used for routing or OOB verification follows the same canonical-input rule, without the Key33 discriminator. Pairwise key distinctness is specified in Section 3.3. Every fixed-size ID MUST have its exact size; ContextID, CID and SID MUST be nonzero.

SID is a 16-byte CSPRNG value. It is collision-checked against present descriptors and pending allocations; no unbounded inventory of historical SIDs is kept. SID is not a UUID string and not the transport SessionID.

## 2.2. Semantic context and trust record

An operator assigns a fresh 32-byte ContextID to each logical application/security domain and provisions it through a trusted configuration channel. ContextID MUST NOT be learned or replaced from an unauthenticated directory response. It is not an IP address, relay hostname, Stream ID or transport connection. Independent applications or security domains MUST use different ContextIDs and independent identity, prekey and state stores. A changed ContextID is a new trust context, not an automatic reconnect.

In an authenticated OOB ceremony, the two people independently verify a fresh 16-byte conversation incarnation CID, both identity keys, and the version and profile. One participant may generate CID with a CSPRNG; both MUST verify the same bytes. The minimum exact public verification transcript is:

```text
TrustTranscript = ASCII("DGProto 2.0 P2 trust") || ContextID:bytes32
               || CID:bytes16 || Encode(IK_low) || Encode(IK_high)
               || 0x02 || LE16(2)
```

`low/high` sort the raw IK bytes in unsigned lexicographic order, not as LE integers. The ceremony MUST also authenticate each key's association with the intended person or contact; matching hashes or a directory account name alone do not verify a person. The OOB channel and UI are outside this wire contract, but their trusted output is mandatory. No TOFU or automatic key replacement is allowed. An unknown or changed key produces TRUST_REQUIRED without erasing healthy state.

The persistent contact key is `(ContextID, CID, local IK, peer IK, version=2, profile=2)`. A local, nonreused verification revision fences asynchronous work. A relay assertion cannot create, modify or approve a contact. CID separates explicitly re-established conversations under healthy identities. Generation serials do not replace CID and are not trust decisions.

## 2.3. Authorization and ownership

Transport static keys MUST be independent of E2EE identity keys. After the Noise handshake, the relay maps the authenticated transport key to exactly one current `account, owner IK, ContextID, OwnerEpoch` and its permitted contacts.

OwnerEpoch is a nonzero u64 assigned by the provisioning authority and never reused for that account within the ContextID. Reassigning the account or replacing the owner IK requires a new epoch. If the epoch allocator is exhausted, admission stops; the allocator MUST NOT wrap.

Registration, credential distribution, account recovery and OOB ceremonies are control-plane operations outside this data-plane specification. Their output MUST supply the mapping above, its authority and a revocation mechanism before any operation is admitted. Handshake success, an empty transport allowlist, a TLS certificate or a server pin is neither contact verification nor mailbox authorization.

The relay MUST check the current owner epoch, the authorized transport credential and the relevant permission at every operation, every cached-result release and every final relay effect commit. A new account owner cannot read journals of a previous owner. The owner can still clean up with DELETE and WITHDRAW after a contact's permission is revoked. Permission changes serialize with admission, commit and result release; bytes already released cannot be recalled. The relay MUST NOT receive private E2EE keys.

## 2.4. Threat and failure model

The confidentiality and authentication goals assume uncompromised endpoints, a correct CSPRNG, a trusted UI, reviewed primitives and a crash-consistent local store without undetected snapshot rollback. Trusted OS time is an explicit lease assumption (Section 9), not a hardware guarantee of rollback resistance.

A hostile relay or network may read public metadata and may substitute, delay, reorder, replay, drop or censor traffic, and lie about every storage result. A dishonest correspondent may send authenticated invalid data. Availability and honest peer behavior are not guaranteed. A compromised endpoint can expose plaintext and live secrets.

The baseline supports process and OS restart under the declared DB/fsync crash model. Live state cloning, backup/restore, concurrent unfenced owners and malicious whole-store rollback are prohibited, and a timestamp does not protect against them. A platform may add rollback resistance without changing the wire, but MUST NOT claim it on the basis of ordinary DB atomicity or of this specification alone.

## 2.5. Terms and identifier namespaces

IK is an identity key, SPK a signed prekey, OPK a one-time prekey and EK the initiator's ephemeral agreement key. In Double Ratchet (DR) notation, DHs/DHr are the local/remote ratchet keys and RK, CK and MK are root, chain and message keys. OOB means out-of-band. E2EE protects content from endpoint to endpoint; hop encryption protects only one endpoint–relay connection.

The table below is a reading aid; allocation, validation and retention rules are in the cited sections. The identifiers are not interchangeable.

| Identifier | Scope and meaning | Rule |
|---|---|---|
| ContextID | Trusted application/security domain; 32 bytes. | §2.2 |
| CID | Independently verified conversation incarnation; 16 bytes. | §2.2 |
| G=(serial,origin) | Ordered E2EE generation within one contact. | §6.1 |
| SID | Random 16-byte E2EE session identifier. | §§2.1, 5.2 |
| message_seq | One sender's u64 message identity across a contact's generations. | §5.3 |
| spk_id / opk_id | Separate u64 allocators for prekeys under one ContextID/IK. | §4.1 |
| OwnerEpoch | Nonreused u64 account-ownership epoch. | §2.3 |
| request_seq | RPC sequence under OwnerEpoch; zero only for ACK_FLOOR. | §7.1 |
| SessionID | Derived 16-byte transport-connection identifier; not SID. | §10.6 |
| Sequence | Transport sequence per direction/key epoch; not message_seq. | §§10.5–10.7 |
| PN / N | Previous DR sending-chain length / message index in the header. | §3.5 |

# 3. Cryptographic profile 2

## 3.1. Selected primitives and exclusions

Profile `0x0002` selects:

- X3DH revision 1 with X25519/SHA-256 and a mandatory OPK;
- the classical clear-header Double Ratchet revision 4, Sections 3.2–3.5;
- the AES-256-CBC/HMAC-SHA-256 message construction in DR Section 7.2.

DR revision 4 Section 7.1 describes PQXDH integration; it does not make X3DH PQXDH. PQXDH, Triple Ratchet, header encryption, tag truncation, alternate ciphers and no-OPK fallback are not part of this profile. [4] [5]

The application session manager (Section 6) is not the Signal wire format and not a wholesale import of Sesame. Profile 2 is deliberately classical: no quantum-safe or harvest-now-decrypt-later protection may be claimed.

## 3.2. Signature and provider contract

`Sig(IK,M)` is XEd25519 as defined in XEdDSA Sections 3 and 5, with SHA-512, exactly 64 output bytes `R || s`, and fresh secret 64-byte CSPRNG input Z for each signing invocation. The signing scalar is the same clamped X25519 private input, reduced modulo q and sign-adjusted by calculate_key_pair; it is not a separately hashed Ed25519 seed. Public-key conversion chooses Edwards sign 0.

The selected verifier:

- rejects u >= p, R.y >= 2^255 or s >= 2^253;
- requires the converted A to be on the curve;
- checks the exact encoded `R = sB−hA` without cofactor multiplication;
- does not additionally require s < q.

An Ed25519 API with a different acceptance set does not satisfy this verifier contract. [6]

The signed prekey message is exactly `Encode(SPK)`, 33 bytes, as in X3DH. It MUST be verified with the independently trusted IK before setup. The signature does not cover numeric prekey IDs, ContextID, time, OPK or account ownership, and those fields MUST NOT be described as signature-authenticated freshness. Profile 2 instead binds ContextID, CID, IDs and setup roles in the message AD, and B checks the IDs against its own live records. [4]

The primitives and XEd25519 MUST come from maintained, reviewable libraries; this document does not authorize a handwritten production primitive. The required API semantics are fixed here. Selecting the provider, version and licence, and independently testing its acceptance set, are implementation gates; this edition selects no production XEd25519 provider. A provider mismatch blocks that implementation; it cannot silently change the verifier or the profile.

## 3.3. Four-DH agreement and DR initialization

A is the generation's immutable initiator and B its responder; these roles are unrelated to transport client and server. Setup contains B's locally allocated SPK/OPK IDs and public keys. IK_A, IK_B, EK_A, SPK_B, OPK_B and the initial DHs_A are pairwise distinct. For fresh setup, A computes:

```text
DH1 = X25519(IK_A.private, raw(SPK_B))
DH2 = X25519(EK_A.private, raw(IK_B))
DH3 = X25519(EK_A.private, raw(SPK_B))
DH4 = X25519(EK_A.private, raw(OPK_B))
SK  = HKDF-SHA-256(salt=00*32,
        IKM=FF*32 || DH1 || DH2 || DH3 || DH4,
        info=ASCII("DGProto 2.0 P2 X3DH"), L=32)
```

B computes the same outputs, in the same order, with the corresponding private keys. A missing, erased or consumed OPK yields PREKEY_UNAVAILABLE, never a three-DH agreement. A initializes `RatchetInitAlice(SK,SPK_B)` with a fresh initial DHs distinct from EK_A; B uses `RatchetInitBob(SK,pair(SPK_B))`. The first ciphertext is produced by RatchetEncrypt, not by a separate cipher keyed directly with SK. B's first response uses the fresh DHs produced by its first receive ratchet step, which randomizes the sending key as X3DH Section 4.3 requires. [4] [5]

A erases EK.private and the intermediate DH outputs after deriving SK. Both parties erase transient SK copies after initialization. B's initialized live RK may equal SK as specified; erasing a transient copy does not erase the live RK. A failed candidate discards its temporary secrets without consuming an OPK or changing live state. The first successful cryptographic receive consumes the OPK atomically, even when the protected body is rejected. The stored public descriptor and initial header remain available, so repeated INITs of that session never rerun X3DH.

## 3.4. Required DR functions and exact labels

All RK, CK and MK values are 32 bytes. HKDF always performs Extract then Expand; it is never Expand with the IKM substituted for the PRK. Every result length and provider failure MUST be checked. IKM lengths are 32 bytes for the root KDF, 160 bytes for X3DH and 32 bytes for message-key derivation. The HMAC inputs below are single bytes, not ASCII. [7]

| Function | Exact definition |
|---|---|
| GENERATE_DH | Fresh X25519 key pair from CSPRNG; canonical public output. |
| DH(pair,pub) | RFC 7748 X25519, with Section 2.1 canonical and nonzero checks. |
| KDF_RK(rk,dh) | HKDF-SHA-256(salt=rk, IKM=dh, info=ASCII("DGProto 2.0 P2 DR Root"), L=64); RK=T[0:32], CK=T[32:64]. |
| KDF_CK(ck) | next CK=HMAC-SHA-256(ck,0x02); MK=HMAC-SHA-256(ck,0x01). Absent CK terminates processing. |
| HEADER | Encode(DHs.public) \|\| LE32(PN) \|\| LE32(N), 41 bytes. |
| CONCAT | AD_message \|\| Header, exactly 443 bytes in this profile. |

```text
T = HKDF-SHA-256(salt=00*32, IKM=MK,
      info=ASCII("DGProto 2.0 P2 DR Message"), L=80)
K_enc = T[0:32]; K_auth = T[32:64]; IV = T[64:80]
CBC = AES-256-CBC(K_enc, IV, PKCS7(Plaintext))
C = CBC || HMAC-SHA-256(K_auth, associated_data || CBC)
```

DECRYPT MUST compare the full 32-byte HMAC in constant time before CBC decryption or unpadding. Strict PKCS#7 adds 1–16 equal bytes, including a full block for block-aligned input. The derived IV is not transmitted. HMAC, CBC and padding failures expose one authentication-failure class, so there is no padding oracle. This instantiates the published CBC/HMAC recommendation; its misuse resistance is not permission to repeat an MK. [5]

At most one ciphertext is ever committed per MK. A retry transmits the stored bytes. After a definitely committed transaction, the endpoint erases the consumed MK, its subkeys and IV, superseded CK/RK/DH-private copies, and consumed skipped keys not needed by live state. An ambiguous commit requires reconciliation, never recovery of an old MK for another encryption. A transaction proven absent committed and emitted nothing, so its keys remain unused (Section 6.2).

Section 8 defines durable ordering and Section 9 defines retention and copy limits. The hop ChaCha20-Poly1305 nonce is unrelated to the derived 16-byte CBC IV of this E2EE construction.

## 3.5. Receive work and skipped-key rules

| Receive bound | Maximum |
|---|---:|
| MAX_SKIP, per chain | 1,000 |
| Skipped keys stored in one ratchet | 1,000 |
| New skipped keys derived by one envelope, both chains combined | 1,000 |
| KDF_CK calls for one envelope | 2,004 |

All ceilings apply simultaneously. The selected session is unique: the receiver MUST NOT search multiple sessions or try alternate keys or profiles on failure. Capacity and work checks run on isolated candidate state. Unauthenticated input MUST NOT evict a skipped key.

A matching skipped key is tried on the candidate. Failure preserves the live key and ends that attempt; it MUST NOT fall through to another chain or DH step. Success consumes the key only in the receive commit, including when the body is rejected. For the current DH, N < Nr without a skipped key is rejected before KDF. An old DH without a saved key can reach only a bounded, failing candidate; no earlier chain snapshot or previously consumed MK may be reconstructed. [5]

PN is the previous sending-chain length; N is captured before Ns is incremented. Transmitted N MUST be < 65,536 and PN <= 65,536. Internal Ns/Nr may reach 65,536; wrap is prohibited. A session permits at most 65,536 encryptions and 65,536 successful cryptographic receives per local endpoint, including controls and rejected bodies. Retries, dedup hits and authentication failures do not count. Section 6 replaces the session before these limits are reached; IK does not change.

# 4. Prekeys and asynchronous establishment

## 4.1. Numeric identities and fresh key ownership

SPK IDs and OPK IDs are separate nonzero u64 counters scoped to `(ContextID,IK)`. Each allocator is durable and strictly increasing. It is never reset by withdrawal, cleanup, account reassignment, time rebase or journal GC. Gaps are permitted and burned forever. At exhaustion, new allocation stops; counters do not wrap. Every new key pair MUST be freshly generated from the CSPRNG, not reconstructed from a persistent master seed that would recover erased past private prekeys.

A public key record is identified by its type, numeric ID and exact Key33. Recipients MUST check all three against live local private-key records. A reused numeric ID with a different public key is not a replacement. Current IK, SPK, OPK, EK and initial ratchet keys MUST satisfy the Section 3.3 distinctness rule. Key generators check for collisions against live and publication-pending keys and abort on RNG failure.

A lifetime list of every historical random public key is not required. Nonreuse rests on permanent numeric-ID exclusion, fresh honest generation and deletion of private material. A relay cannot certify that a malicious owner never repeats an old public key under a new ID. Such an owner violates this profile, but exact detection of that behavior is not promised indefinitely. Random key collision remains a negligible-probability cryptographic assumption, not a guarantee checked against finite history.

## 4.2. Bundle and publication bytes

```text
Bundle = Encode(IK_B) || spk_id:u64 || Encode(SPK_B)
      || signature:bytes64 || opk_id:u64 || Encode(OPK_B)
OPKEntry = opk_id:u64 || Encode(OPK)
Publish = Encode(IK) || spk_id:u64 || Encode(SPK)
       || signature:bytes64 || count:u16 || OPKEntry[count]
```

Bundle is 179 bytes. Publish is `140 + 41*count` bytes, with count 0–100.

- An identical active `(spk_id,SPK,signature)` may accompany new OPKs without refreshing its age.
- A new SPK ID MUST exceed the persistent published-SPK high-water mark; a retired ID cannot be reactivated.
- Every OPK ID in a new logical publication MUST exceed the published-OPK high-water mark, and the entries MUST be strictly increasing.
- The relay atomically advances the marks and adds the pool records, even if some IDs were skipped.
- A retry of the same RPC reuses its journal entry and does not republish keys.

Endpoints serialize distinct PUBLISH operations so that out-of-order publication does not burn their own earlier pending batch. A new logical publication containing an old ID is CONFLICT, including after WITHDRAW, expiry, journal GC and account changes; the high-water marks survive all of these. A retired IK cannot be enrolled as a fresh identity merely to clear its marks. Publication state and the available pool are separate from the persistent exclusion marks.

## 4.3. Lifetimes and availability budget

One day is 86,400 seconds of the trusted lease time defined in Section 9. First-publication ages never refresh on retry or republication. Endpoints publish fresh keys within 24 hours of generation; a stale queued publication MUST be abandoned, not given a new creation time. Endpoints SHOULD rotate the SPK at five days and keep public and private headroom before depletion. Directory inventory is an availability hint, not trusted evidence of remaining private lifetime.

| Material | Controlling maximum / deadline |
|---|---|
| Active SPK at endpoint | Replace by generation + seven days. |
| SPK issuance at relay | Stop new reservations at first publication + six days, or WITHDRAW/replacement, whichever first. |
| Previous SPK private key | Delete at min(replacement + 14 days, generation + 21 days). |
| OPK private key | Delete at generation + 30 days; successful consumption deletes earlier. |
| Unissued public OPK | Delete at first publication + 21 days; never return an issued key to the pool. |
| Use of successful fetched bundle | First INIT commit by FETCH logical creation + one hour. |
| FETCH logical request | Three attempts, deadline creation + ten minutes; a late result cannot start a cancelled/expired sequence. |

Fresh publication, the 21-day public OPK age and the one-hour bundle-use window leave margin before the 30-day private limit for a seven-day mailbox interval. They do not guarantee that an offline recipient still holds the key, or that a malicious relay applies correct ages. SPK availability requires recent publication, so an endpoint absent for longer than the issuance window may prevent new sessions. Existing healthy sessions do not need an available SPK/OPK until renewal. No-OPK fallback is prohibited, including during long offline periods.

## 4.4. Reservation, consumption and repeated INIT

FETCH_BUNDLE atomically removes one unissued OPK, saves the exact Bundle and commits its journal result before releasing it. A reservation is never returned to the pool after timeout, cancellation, expiry, restart or ACK_FLOOR. A retry with the same account epoch, sequence and bytes returns the same saved result while it is retained and does not spend another OPK. A new logical FETCH is a new reservation under the rate limits.

B resolves IDs and public keys against its own live records and rechecks trust, expiry and nonconsumption at the successful first receive commit. That transaction consumes and deletes the OPK private material, installs the session and descriptor, and records the accepted or rejected body disposition. Concurrent candidates using one OPK race on the same private record and only one commits; the losers reload and expose nothing. An authentication failure consumes nothing.

Once a session descriptor is known, a repeated matching INIT uses the session's DR state and saved initial header, not X3DH or the deleted OPK. A conflicting descriptor cannot reset that state. If the session has retired, its floor/seen marker prevents recreation. A missing or lost reservation or a rejected old setup may be replaced by a *higher generation* under Section 6, without claiming that the earlier message was not accepted. No old plaintext is automatically reencrypted in the new session.

# 5. Exact E2EE wire format

## 5.1. Envelope grammar and offsets

```text
Envelope = version:u8 || profile:u16 || kind:u8
        || ContextID:bytes32 || CID:bytes16
        || serial:u64 || origin:bytes32 || SID:bytes16
        || sender:bytes32 || recipient:bytes32 || C_len:u16
        || Setup_if_INIT || Header:bytes41 || C:bytes[C_len]
Setup_if_INIT = Setup:bytes181 if kind=1; empty if kind=2
Setup = Encode(IK_A) || Encode(IK_B) || Encode(EK_A)
     || spk_id:u64 || Encode(SPK_B) || opk_id:u64 || Encode(OPK_B)
Header = Encode(DH_sender) || PN:u32 || N:u32
```

Only version 2, profile 2, kind 1 (INIT) and kind 2 (REGULAR) are permitted. Generation G is `(serial,origin)`; origin MUST equal A's raw IK. sender and recipient give this packet's actual direction, so they are not always A and B.

- INIT: sent only by A, and only before any authenticated response on that session. It has sender=A, recipient=B, PN=0, N=0–31 and the original DHs_A. Every INIT for a descriptor repeats the same Setup and initial DHs_A.
- REGULAR: requires a known stored descriptor and contains no Setup. B always sends REGULAR.

Both endpoints can send any permitted content type on the active bidirectional session.

| Field | Offset | Bytes |
|---|---:|---:|
| version / profile / kind | 0 / 1 / 3 | 1 / 2 / 1 |
| ContextID / CID | 4 / 36 | 32 / 16 |
| serial / origin | 52 / 60 | 8 / 32 |
| SID | 92 | 16 |
| sender / recipient | 108 / 140 | 32 / 32 |
| C_len | 172 | 2 |
| Setup, INIT only | 174 | 181 |
| Header | 174 REGULAR; 355 INIT | 41 |
| C | 215 REGULAR; 396 INIT | C_len |

C_len is 48–32,816 inclusive, with `(C_len−32) mod 16 = 0`; a valid empty protected body fits C_len=48. A REGULAR envelope is 263–33,031 bytes and an INIT envelope 444–33,212 bytes. The decoder MUST verify the complete packet length before DH or allocation. Unknown constants, noncanonical keys, zero required IDs, wrong context or orientation, wrong lengths and extra bytes are rejected, never negotiated.

## 5.2. Stored descriptor and associated data

```text
AD_session = ASCII("DGProto 2.0 P2") || 0x02 || LE16(2)
          || ContextID || CID || LE64(serial) || origin || SID
          || Setup || Encode(initial_DHs_A)
AD_message = AD_session || sender || recipient || kind || LE16(C_len)
associated_data = AD_message || Header
```

| Value | Bytes |
|---|---:|
| Label `"DGProto 2.0 P2"` | 14 |
| AD_session | 335 |
| AD_message | 402 |
| associated_data | 443 |

The descriptor is `(version,profile,ContextID,CID,G, SID,Setup,initial_DHs_A)`. Its exact canonical bytes are stored with the ratchet. REGULAR uses that saved descriptor; no directory input can supply a replacement Setup. Before decrypting, the receiver MUST check that the envelope's version, profile, ContextID, CID, serial, origin and SID (and, for INIT, Setup) match the descriptor. sender, recipient, kind and C_len are not descriptor fields: they are validated under Section 5.1 and authenticated through AD_message. C_len is determined from the plaintext length and PKCS#7 before encryption.

This is a fixed-length, unambiguous AD construction built on X3DH's application AD extension and DR's HEADER/CONCAT interface. It authenticates ContextID, CID, profile, generation, roles, setup IDs and keys, actual direction, kind, length and header. Authenticated is not the same as fresh: contact verification supplies trust, and monotone state supplies the admission and replay rules. [4] [5]

RPC sequence, OwnerEpoch, status, TTL, arrival cursor, IP, namespace tuple and transport sequence are not E2EE facts. Moving the *same* envelope through a future compatible binding MUST NOT change its AD or bytes. A different ContextID or CID MUST fail; a physical carrier change MUST NOT be mistaken for a new conversation.

## 5.3. Protected body and canonical message identity

```text
Plaintext = content_type:u8 || message_seq:u64 || body_len:u16
         || body:bytes[body_len]
ReceiptBody = target_seq:u64 || receipt_kind:u8
```

Plaintext is exactly `11+body_len` bytes, with body_len 0–32,768.

message_seq is a nonzero u64 that the sender allocates monotonically across *all* sessions for the contact, including controls and receipts. It is not a ratchet N, generation serial, nonce or request sequence, and it MUST NOT reset on ordinary renewal. The canonical message identity and effect key is `(ContextID,CID,sender IK,message_seq)`. Profile 2 has no random message_id field; a local SDK submission handle may exist as separate metadata. At sequence exhaustion the sender stops new encryption, and the explicit new-CID ceremony of Section 11 is required. No counter wraps and no undocumented sequence reset is allowed.

| content_type | Body rule and meaning |
|---:|---|
| 1 | Strictly valid UTF-8 text, including empty. No replacement decoding or implicit normalization. |
| 2 | Opaque bounded bytes with separately agreed safe application semantics. |
| 3 | Exactly nine-byte ReceiptBody; receipt_kind 1 DECRYPT_COMMITTED or 2 APPLICATION_COMMITTED. |
| 4 | SESSION_READY, exactly empty body. Confirms cryptographic session progress only. |

Other content types and receipt kinds are rejected after authenticated decryption. SESSION_READY and receipts each get their own fresh message_seq and MK, and neither generates a receipt. Accepting type 1 or 2 creates a DECRYPT_COMMITTED intent. APPLICATION_COMMITTED is optional and requires the durable effect capability of Section 8.

A receipt target MUST identify the receipt receiver's own earlier type-1/2 send in this CID; the peer IK is implicit from the authenticated packet. A receipt may travel over a newer active session and acknowledge a message sent in an older one. A target without retained send-status metadata is UNKNOWN_TARGET; it is not a recreated send or a reason to reset. APPLICATION_COMMITTED is stronger than DECRYPT_COMMITTED, and a later weaker receipt cannot regress the status. Neither means that a human read the message. Repeated statements are idempotent. Receipt creation is unique by `(contact,target_seq,receipt_kind)`, independent of the carrying SID.

## 5.4. Bounded cross-generation replay window

For each contact's incoming direction, the endpoint maintains a durable `rx_high:u64` (initially 0) and a 4,096-bit seen bitmap whose bit 0 corresponds to rx_high. Positions outside `max(1,rx_high−4095)..rx_high` are permanently stale, not forgotten-but-admissible. For a successfully authenticated body whose first nine bytes are complete and whose message_seq is > 0, the endpoint evaluates the window as follows, even if later body validation fails:

1. If seq > rx_high, shift the bitmap by `seq−rx_high`, clearing it if shift >= 4,096, and set rx_high=seq. Do not loop over the numeric gap.
2. If seq <= rx_high and `rx_high−seq >= 4,096`, reject the body as STALE_WINDOW. This does not claim that the older message was ever delivered or processed.
3. If the in-window bit is already set, reject as AUTHENTICATED_REPLAY with no further application or receipt effect. Otherwise set it and record the accepted or rejected disposition in the receive transaction.

When storage is available, successful cryptographic DR advancement always commits, even for a rejected body. Unauthenticated bodies, and bodies truncated before message_seq, do not advance the window, but their authenticated DR disposition is still committed. Sequence 0 is invalid and never advances the window. A valid authenticated seq consumes its one-time window position even if its type, length, UTF-8 or receipt is invalid.

No contiguous committed prefix is claimed: gaps may be cancelled, never emitted, lost or delayed. Advancing the window may reject a very late valid message. It MUST NOT cancel an already accepted pending effect, imply the absence of an earlier remote effect, or delete unknown-outcome application records. Inbox and effect state have their own retention and admission rules. The window bounds replay history; it does not turn missing sequence numbers into decisions.

# 6. Session generations, concurrency and retirement

## 6.1. Total order and permanent high-water state

For a contact, G=(serial,origin) has serial 1..2^64−1 and origin equal to one of the two verified raw IKs. Generations are compared first by serial as an unsigned number, then by raw origin bytes in unsigned lexicographic order; the **greater** tuple wins. Implementations MUST NOT compare LE serial encodings lexicographically, or use random SIDs, account names or timestamps. A and B are determined by the origin of the generation and never swap in replies.

The endpoint persists M, the greatest generation it has ever created locally or admitted cryptographically, initially (0,none). M never decreases, even after every key of that generation is gone. The endpoint also retains a two-origin seen/retired mask for serials M.serial and M.serial−1 (at most four bits) and the local lower-admission deadline for M.serial. Seen means the generation has been admitted or deliberately retired, not that every message or effect completed. A stored descriptor identifies a present slot; a generation that is seen but absent cannot be recreated.

Only the slot at G=M, if present, permits new encryption. At most four ratchets per contact may retain keys: the active slot plus at most three DRAINING slots. A lower slot is always receive-only. A marker for M without its keys is legal; the contact then creates M.serial+1 instead of reopening M. At serial exhaustion no new generation can be allocated, and an explicit new CID is required.

## 6.2. Creating a local generation

Only the verified local identity may create a generation with its own origin. When setup or renewal is needed, the endpoint obtains a valid unused fetched bundle, chooses serial=M.serial+1 and generates a fresh, collision-checked SID, EK and initial DHs. In the send commit it rechecks the current M, trust, key ages, resource budget and ownership. It then atomically commits the new session and descriptor, M and masks, the next message_seq, the encrypted first INIT, the outbox/RPC entry and the bundle-to-SID consumption. Only then does it emit. A stale candidate is discarded. An ambiguous prior commit is reconciled, never recomputed from old sending state.

Local creation immediately makes the new generation the local encryption choice. It is not a two-party COMMIT, nor an assertion that the peer accepted anything. The previous active slot is demoted and Section 6.5 applies. Once a descriptor might have been emitted, its message_seq and ciphertext cannot be reassigned or reencrypted. A later higher generation is a new session, not a cancellation of the old one.

An endpoint creates at most one generation per contact in any rolling 60-second interval, and reserves durable time/counter admission before any cryptography. Receiving an authentic higher generation is not subject to this local creation rate, but remains subject to receive work and admission limits. A failed or ambiguous FETCH cannot justify no-OPK setup. A local creation attempt consumes its generation only when the atomic first-send transaction commits. A transaction proven absent emitted nothing, so the attempt may be performed again.

## 6.3. Admission of an incoming INIT

For an existing known G/SID, the endpoint requires the identical descriptor and initial DHs and uses that DR state; a mismatch is CONFLICT. REGULAR never creates a slot. For an unknown INIT, the endpoint first validates context, CID, verified roles, serial, public grammar, local SPK/OPK records and prospective capacity, then computes on a candidate. Only successful cryptographic authentication can change M, masks or slots.

| Incoming unknown generation | Admission after successful cryptographic authentication |
|---|---|
| G > M | Install as active, advance M, shift seen masks to the new two-counter window and demote old active. |
| G <= M, serial in {M.serial,M.serial−1}, unseen origin bit, before lower-admission deadline | Install receive-only DRAINING if it survives the four-slot capacity rule; set its seen bit, never lower M. |
| Same generation already seen but no keys | Reject RETIRED. Never rerun X3DH or restore its OPK. |
| Serial below M.serial−1 and no known retained slot | Reject STALE_GENERATION. No marker for every old random SID is needed. |
| Conflicting SID/descriptor for an existing G | Reject CONFLICT; preserve current state. |

When M.serial first advances, the lower-admission deadline is set to local time + seven days. A same-serial origin tie-break, duplicate, old traffic or retry does not extend it. Its expiry closes admission of unseen lower or equal generations for that serial; a later *higher serial* gets a new deadline. Shifting the masks drops only entries that fall below the numeric floor. Known retained older slots may still decrypt until their own retirement, but cannot be recreated after erasure.

If all slots are full, the endpoint keeps the four greatest G tuples among the existing slots and the authenticated candidate. An unknown lower candidate that cannot survive this selection is rejected before any cryptography, with no mutation and no OPK consumption. A higher candidate may evict the smallest slot, but only in its successful atomic commit. Unauthenticated input never displaces keys. Capacity limits and expiry may lose old messages; retirement does not prove that those messages had no earlier remote effect.

## 6.4. Confirmation and simultaneous initiation

A locally initiated active slot is INIT_PENDING. It may encrypt at most 32 INITs on its original initial chain (N=0–31), including controls; retries do not count again. The first successful cryptographic response from B, even with a rejected body, permanently ends the initial phase, and later new sends on that slot are REGULAR. This keeps post-response DH headers out of the INIT grammar. An authenticated invalid body does not falsely confirm an application message.

B's first authenticated INIT installs a session even if the body is rejected. For a new active responder slot, B MUST create one durable SESSION_READY intent, unless its first ordinary outbound message on that SID already provides the response. Any first committed outbound message on that SID fulfills the intent; it uses B's fresh receive-ratchet DH. A READY intent is fulfilled only on its own SID. If that SID stops being the active session before fulfillment, the intent is suppressed and no READY is sent on another SID: A learns of the greater session from traffic on that session. An INIT accepted into a DRAINING slot creates no new ciphertext there; receipt intents for user messages may use the current active session instead. No response is sent for an authentication failure.

Simultaneous creation by A and B may produce `(n,IK_A)` and `(n,IK_B)`. Both are genuine, independent sessions. At any endpoint that knows both, the greater tuple is active; the lower one can receive delayed ciphertext but never becomes active through that traffic. Subsequent sends use the greatest known session. Ratchets are not merged, and no acknowledged global COMMIT is required. If the greatest admitted INIT is delivered and no further replacements occur, both endpoints converge on the same choice. A hostile network can prevent that delivery, so unconditional liveness is not claimed.

## 6.5. Draining, expiry and retirement

On demotion, or on first admission as receive-only, a slot receives a nonextendible drain deadline of local time + seven days. The slot may decrypt bounded delayed messages and process accepted inbox data, but MUST NOT allocate new encryptions. Ciphertext already committed from the slot may still be retried until its original deadline, under current trust and authorization. Receipts required for its messages are new encryptions in the current active slot or in a freshly created higher generation, and they keep the old target_seq.

A slot is retired on any of:

- its drain deadline;
- four-slot eviction;
- a hard receive limit;
- explicit authorized local session disposal;
- required lease cleanup.

Retirement erases the slot's RK, CK, DH-private and skipped-key material. M and the required current/previous seen bits are preserved, and absence below the floor permanently excludes reopening. The SID and descriptor hash of the current M may be kept as bounded diagnostic metadata, not as live secret state. Slot retirement does not retire the CID, IK, received effects or unknown-outcome work.

No delayed COMMIT or ACK can resurrect erased state. A late INIT or REGULAR for a retired generation is rejected. A late receipt reconciles retained status only if it authenticates in a still-valid session, and it cannot reactivate its carrier. A lost confirmation or old-state expiry may leave a message UNCONFIRMED. Only a new higher-generation descriptor can resume session work under healthy trust and state.

## 6.6. Routine renewal and conditional progress

Before any new encryption, if the active slot's cumulative send count or successful receive count is >=60,000, the endpoint MUST use a fresh higher generation instead. At 65,536 successful receives the last transaction may commit, and the slot then retires. No new encryption is permitted at N>=65,536, at PN>65,536 or at a hard count. Receipts, SESSION_READY and rejected authenticated bodies consume their specified counts.

If an INIT_PENDING slot reaches 32 encryptions, or one hour passes without an authenticated response, new encryption in it is suspended. When further outbound work needs a session, the endpoint creates a higher generation within the rate and prekey limits. Existing immutable outbox entries may continue their own bounded retries. A peer's timeout, error or unknown SID does not by itself retire a healthy local active session or alter trust. Stale unconfirmed setup is handled by the local replacement policy, and no old message is silently copied into the replacement.

A healthy restart preserves the committed active DR state, M, the send sequence and the replay window. A transport reconnect does not create a session. Normal SPK rotation does not disturb established DR state. After an ordinary key or overlap cut, new setup requires fresh prekeys, not new identity keys. If private prekeys are unavailable, new-session progress waits; established receive work may continue where permitted.

Progress is conditional on fair delivery, valid trust, keys and time, available durable capacity, and the absence of permanent endpoint or relay failure. Four-slot eviction, the seven-day mailbox and overlap limits, the 4,096-sequence window and expired skipped keys all permit loss. The protocol does not promise transparent recovery of every old ciphertext, indefinite offline catch-up, or successful re-execution of unknown external effects.

# 7. Relay RPC and bounded operation journal

## 7.1. Exact RPC header

```text
RPC = magic:bytes4 || version:u8 || profile:u16 || flags:u8 || op:u8
   || ContextID:bytes32 || OwnerEpoch:u64 || request_seq:u64
   || payload_len:u16 || payload:bytes[payload_len]
ResponsePayload = status:u8 || result
```

| Field | Offset | Bytes | Value |
|---|---:|---:|---|
| magic | 0 | 4 | Exactly `44 47 52 32` (ASCII `DGR2`). |
| version | 4 | 1 | 2. |
| profile | 5 | 2 | 2. |
| flags | 7 | 1 | 0 = request, 1 = response; no other value. |
| op | 8 | 1 | Section 7.2. |
| ContextID | 9 | 32 | Checked on every packet. |
| OwnerEpoch | 41 | 8 | Owner epoch of the account. |
| request_seq | 49 | 8 | See below. |
| payload_len | 57 | 2 | Length of the payload that follows. |

The header is 59 bytes, and every packet is exactly its stated remainder long. A response payload starts with a status byte. Only an OK response carries the operation-specific result; every non-OK payload is the single status byte. Every response repeats the original request's ContextID, OwnerEpoch, op and request_seq.

A normal request_seq is a nonzero u64, allocated durably and strictly increasingly across all account operations and all shared workers and connections. It is not random, and it is neither a transport sequence nor an E2EE message sequence. Every retry preserves that request_seq and the exact immutable RPC bytes. Only `ACK_FLOOR` uses request_seq = 0. request_seq never wraps or resets under an existing OwnerEpoch.

## 7.2. Operation and status registries

| op | Request payload | Request bytes | OK result after status | Result bytes |
|---:|---|---:|---|---:|
| 0 ACK_FLOOR | floor:u64; request_seq=0 only | 8 | requested_floor:u64 \|\| committed_floor:u64 | 16 |
| 1 PUBLISH | Publish from Section 4.2 | 140+41*count | Empty | 0 |
| 2 FETCH_BUNDLE | recipient:bytes32 | 32 | Bundle | 179 |
| 3 STORE | One Envelope | 263–33212 | Empty | 0 |
| 4 POLL | Empty | 0 | One Envelope | 263–33212 |
| 5 DELETE | sender:bytes32 \|\| CID:bytes16 \|\| SID:bytes16 \|\| Encode(DH_sender) \|\| N:u32 | 101 | Empty | 0 |
| 6 WITHDRAW | Empty | 0 | Empty | 0 |

DELETE names an item in the caller's own mailbox; the recipient is taken from current authenticated owner state. The public index of a STORE is `(recipient, sender, CID, SID, DH_sender, N)`; the complete immutable bytes, or their digest, bind its value. ContextID is implicit in the service's storage partition and is checked on every packet.

| status | Name | Meaning |
|---:|---|---|
| 0 | OK | Operation-specific committed result, not E2EE delivery. |
| 1 | EMPTY | No usable bundle for FETCH_BUNDLE or eligible item for POLL; only these operations. |
| 2 | UNAUTHORIZED | Current account/epoch/credential/contact permission failed. |
| 3 | MALFORMED | Bad grammar/constants/encoding or invalid public signature. |
| 4 | QUOTA | Admission, rate, byte or count budget prevents progress. |
| 5 | CONFLICT | A sequence/index/ID has conflicting bytes or another POLL is unresolved. |
| 6 | EXPIRED | Sequence <= `ack_floor`, retained result expired, or operation material/deadline expired. |
| 7 | UNAVAILABLE | No new success asserted; service/outcome reconciliation unavailable. Not proof that a previous attempt had no effect. |

Unknown magic, version, profile, op, flags or status values, and trailing data, are errors; they do not negotiate extensions. If no usable correlation header can be parsed, no correlated response is fabricated: reject the packet or close the hop under bounded admission. Local E2EE or secret-state errors are never substituted for relay statuses.

## 7.3. Sliding journal, exact retries and permanent exclusion

Each owner epoch has a durable `ack_floor`, initially 0, and at most 64 normal sequence slots. A new request is admitted only if `ack_floor < seq <= ack_floor+64`, evaluated with checked arithmetic. Data operations (PUBLISH, FETCH_BUNDLE, STORE) may occupy at most 48 retained slots, so at least 16 slots always remain available to the control operations POLL, DELETE and WITHDRAW. Control operations may use any free slot. A deployment with lower admission limits discloses them; it cannot remove the separate `ACK_FLOOR` path or owner-cleanup safety.

Within the admitted window, the first staged record for a sequence binds the op and all exact request bytes (or a pinned collision-resistant SHA-256 digest plus the payload required for recovery). The same sequence with different bytes is CONFLICT. The same bytes join or reconcile the same logical work and cannot run a second effect. Staging is bounded and does not constitute success. On successful execution, the effect, exact response, prekey reservation or mailbox cursor, and completed journal disposition commit atomically before the result is released.

OK and EMPTY are retained as exact observations; terminal authenticated failures may be retained. QUOTA and UNAVAILABLE are not completed effects and cannot replace a committed success. A staged retryable request may be retried with its original bytes while it remains admitted. No malformed, unauthorized or resource-failure input may force unbounded journal creation. An unknown database commit outcome quarantines that work until it is reconciled; no other request or worker may execute its effect again. A partial effect/result record violates atomicity and blocks the storage owner.

Eight days after first staging or commit, the request and result payload bytes may be erased. The slot is kept, holding the sequence, request digest and expired or final disposition, until `ACK_FLOOR` covers it. Such a sequence returns EXPIRED and never executes anew. Under time uncertainty, payloads may be expired early, provided the slot and its exclusion are preserved. A completed POLL may therefore retain ciphertext beyond the active mailbox TTL; this retention MUST be disclosed. The malicious-relay model allows indefinite retention, whatever the honest-service policy.

## 7.4. ACK_FLOOR and abandoned sequence holes

`ACK_FLOOR` is an authenticated, idempotent, monotone control operation outside the 64 journal slots. It sets `ack_floor=max(old_floor,requested_floor)` and atomically removes all slots at or below the floor. Its OK result carries both `requested_floor` and the actual `committed_floor`. It needs no response journal and consumes no normal slot. One `ACK_FLOOR` worker per account coalesces newer floors; late responses cannot regress the floor.

Before issuing a floor, the endpoint MUST durably stop all scheduling at or below it and record each covered result as processed, terminal, or explicitly abandoned with its possible prior effects still uncertain. Holes that were allocated but never emitted may be crossed. A floor does not claim a contiguous prefix of application commits. The normal sequence allocator MUST remain strictly above every emitted floor.

Every operation's final effect commit checks the *current* floor in the same transaction, so an uncommitted stale worker at seq <= floor commits no effect. A commit that won the race against `ACK_FLOOR` remains committed: `ACK_FLOOR` does not undo a STORE, restore an OPK, cancel a peer effect or establish nondelivery. Garbage collection never lets an old sequence become a fresh operation. A retry with seq <= floor returns EXPIRED after authorization. An unknown earlier effect is not retried under a new sequence merely to obtain an answer.

If the journal is full, completed or abandoned slots can be released with `ACK_FLOOR`, and owner cleanup continues on the reserved path. `ACK_FLOOR` itself still performs bounded authentication, rate and storage work. Physical disk failure or an irreconcilable database outcome may block all work; reserved capacity cannot manufacture a durable commit. A normal disconnect or cancellation is not an `ACK_FLOOR` unless the durable client procedure above has actually run.

## 7.5. Operation effects and prekey authorization

**PUBLISH** validates the current owner IK, the signature, pairwise uniqueness of the submitted keys, IDs, count and order, relay-observable original publication ages, and pool capacity. The endpoint enforces generation-to-publication age locally: no generation timestamp is carried, and the relay cannot certify when a key was generated. High-water advancement, active SPK selection, public-key insertion and journal commit are atomic. `count=0` permits SPK rotation without OPKs.

**WITHDRAW** removes the current published SPK and unissued OPKs. It does not remove published-ID high-water marks, issued reservations, mailbox items or contact trust.

**FETCH_BUNDLE** validates that the requester is a permitted contact of the recipient, the availability and age of the SPK and OPK, and the per-pair and per-recipient reservation quotas, then reserves exactly one OPK atomically. An issued key cannot be reissued after `ACK_FLOOR` or expiry. A stale saved Bundle may be replayed as an old observation, but the endpoint still obeys its one-hour use window and Bob enforces private-key validity. The relay never attests private-key retention or successful E2EE setup.

**STORE** requires the envelope sender to be the owner IK and the recipient to be a permitted contact. It validates the public envelope shape, context, keys and limits; it does not check HMAC or plaintext. The same public index with identical bytes returns OK without refreshing the TTL; differing bytes return CONFLICT. A duplicate OK may refer to a deletion or expiry tombstone and need not restore a retrievable item. Mailbox admission, the STORE-index digest and the journal effect/result MUST commit together.

**DELETE** requires current mailbox ownership and removes only the named index; deleting an absent item is an idempotent OK. Contact revocation MUST NOT prevent owner cleanup. DELETE does not prove decryption or processing, and WITHDRAW is likewise owner cleanup, not identity revocation. Their response bytes and journal admission obey the same atomicity and floor rules as other normal requests.

## 7.6. Fair mailbox observation and concurrent POLL

For each admitted new STORE, assign a never-reused, monotone u64 arrival ordinal. Maintain a durable round `cursor` and `round_cut`. A fresh successful POLL selects the lowest eligible ordinal greater than `cursor` and at most `round_cut`. If none exists, start a new round with `round_cut` set to the current highest arrival ordinal and `cursor=0`, then select that round's lowest eligible item, or return EMPTY. An item is eligible if it is unexpired and from a currently permitted sender. Arrivals above `round_cut` wait for the next round. Selection and the cursor/journal result commit atomically.

When the mailbox holds at most M items (M <= 1,000) and processing, authorization and storage remain available, every continuously eligible, unexpired stored item is visited within at most 2*M fresh nonempty selections. The bound counts selections, not seconds, network attempts or latency. A cached POLL neither advances `cursor` nor counts as a fresh selection. A persistent poison item can reappear each round but cannot monopolize all fresh observations. Neither FIFO ordering nor absence of duplicates is implied.

At most one distinct unresolved POLL logical sequence may execute per account, across all connections. A request with identical sequence and bytes joins or reconciles it; another unresolved POLL sequence is CONFLICT. An `ACK_FLOOR` past an uncommitted old POLL fences its future commit, after which a new POLL can proceed. A completed POLL is a saved observation, not a subscription. Before every cached or fresh release, recheck current owner and sender permission; revocation returns UNAUTHORIZED, not the old ciphertext or a different selection. DELETE can clean up revoked or poison items without decrypting them.

## 7.7. Durable retries, cancellation and late observations

Every logical request record binds the ContextID and owner epoch, exact bytes, sequence, trust and binding revisions, dependency, creation time and deadline, scheduler revision, attempt count, next eligibility time, in-flight timeout and monotone result facts. Before handoff, the owner commits an attempt increment and timeout under the current fence and permissions. A crash after authorization consumes that attempt; only a handoff proven rejected before authorization may avoid consuming it. Waiting while known to be offline consumes elapsed time but is not a transmission attempt.

The normal per-attempt timeout is 30 seconds. Attempt limits and absolute deadlines are:

| Operation | Maximum attempts | Absolute deadline |
|---|---:|---|
| STORE, PUBLISH, DELETE, WITHDRAW | 256 | Creation + seven days |
| FETCH_BUNDLE | 3 | Ten minutes |
| POLL | 3 | Two minutes |
| ACK_FLOOR, per chosen floor | 256 | Seven days |

Coalescing a later floor does not reopen an abandoned old logical effect. Worker queues and concurrent tasks are bounded. Each new POLL observation is a fresh logical request, not an indefinite retry of one ID.

Before retry k>=2, wait base=`min(2^(k−2),21600)` seconds plus fresh uniform CSPRNG jitter in [0,base/4]. Persist the sampled delay once. The deadline takes precedence. Going offline or restarting does not refresh the creation time, attempts, delay or deadline. The first base delay is one second and the base is capped at six hours; evaluate the cap before exponentiation, so for k>=17 the base is 21,600 seconds. With jitter, the maximum complete delay is 27,000 seconds (7.5 hours), still subject to the original deadline. The seven-day outer age limit does not guarantee coverage of an outage or delivery near the deadline.

Retry the same bytes and sequence only after transport ambiguity or a QUOTA or UNAVAILABLE response. OK, EMPTY, a terminal status, a valid endpoint receipt, exhaustion or user cancellation durably stops the corresponding scheduler. Stale callbacks cannot restart it, refund attempts or resample its delay. Because the wire carries no attempt number, a late transient response cannot be attributed to a later attempt to bypass the persisted timer. Reconcile an ambiguous database result before scheduling anew.

Record responses durably before events or workers use them. Identical repeats cause no second Bundle use, POLL event or application effect. A late OK may resolve retained uncertainty but cannot recreate retired sessions, restart workers or reissue OPKs. An endpoint receipt may stop a STORE before its relay reply; a later relay failure cannot weaken the receipt. A current UNAUTHORIZED may supersede permission without undoing a historical OK. Likewise, EXPIRED after an acknowledged floor or result-retention cut does not undo an earlier result. Other divergent completed results are retained as bounded conflict evidence, and their automatic use is suspended.

## 7.8. Acknowledgement semantics

Three distinct mechanisms acknowledge something in DGProto: the relay `ACK_FLOOR` (Section 7.4), the transport `Ack` (Section 10.8) and endpoint receipts (DECRYPT_COMMITTED, APPLICATION_COMMITTED). They are independent, and none implies another.

| Observation | Exact meaning and limit |
|---|---|
| Queue acceptance, socket write, transport `Ack` | Hop fact only; no relay or endpoint commit. |
| STORE OK | Honest relay's durable operation assertion; may refer to an old tombstone; hostile relay may lie. |
| SESSION_READY / authenticated response | Peer has cryptographic state sufficient for that response; not application-message acceptance. |
| DECRYPT_COMMITTED | Peer asserts accepted durable inbox commit of the targeted type-1/2 message. |
| APPLICATION_COMMITTED | Peer asserts a defined durable effect under its idempotency contract; not human reading. |
| DELETE OK | Owner cleanup at relay, not an E2EE receipt. |
| Timeout, cancellation, EXPIRED, no receipt | Unconfirmed outcome; not proof no remote effect occurred. |

Expose scheduling, relay submission and authenticated endpoint status as separate facts. Do not label an unconfirmed submission definitively UNDELIVERED in a UI or API; UNCONFIRMED remains accurate if an old message expires or a response never arrives.

# 8. Durability, effects and recovery

## 8.1. Single owner and durable schema

One fenced storage owner serializes identity, prekey, contact and session mutations across workers and connections. Live identity cloning and two unfenced processes are prohibited. A detached transport handler may outlive connection shutdown, so Close does not prove that its storage writer stopped. Every final mutation or exposure atomically checks the current owner fence and the exact dependency revisions.

The durable schema includes:

- version and compatibility pins; ContextID and owner epoch;
- contact CID and verification revision;
- M, masks and deadlines; immutable descriptors;
- DR state, skipped keys with their ages, and counters;
- the global per-contact message_seq and replay window;
- prekey allocators, private records and published marks;
- inbox and outbox, effect records and receipt intents;
- RPC sequences, floors and schedulers;
- required retirement records.

Either hold exclusive serialization from snapshot to commit, or compare every shared version read in the successful atomic transaction. Send, receive, expiry and trust races invalidate stale candidates. Recheck trust, counters, ages, capacity, owner and slot permission at commit, not merely before expensive crypto. Writing an old computed snapshot to the database is not a valid serialization mechanism.

## 8.2. Send and receive transaction order

For a new logical send, allocate message_seq and a sending step on isolated state and encrypt once. Then atomically commit the advanced DR state together with the immutable envelope, outbox entry, RPC/scheduler record and submission-handle mapping. Bytes may enter the transport queue only after a definite durable commit. An ambiguous commit is reloaded and reconciled; if the outbox entry exists, reuse it. An absent transaction may be recomputed only after both its absence and its nonemission are established. Never restore an older CK or MK.

The receive path MUST use this order:

1. Validate carrier bounds, authentication, replay and binding grammar. Validate RPC correlation and constants, envelope lengths, context and keys, and work budgets before any peer-driven DH or allocation. Snapshot immutable input ownership.
2. Check current trust and CID, recipient and roles, generation admission and descriptor, cached index/dedup, key availability and ages, and durable capacity.
3. On an isolated candidate, perform bounded X3DH/DR. Verify HMAC before decryption and strict unpadding. On any cryptographic failure, discard the candidate and release nothing.
4. After successful crypto, validate the body, sequence replay window, type, length, UTF-8 and receipt target. Commit the successful DR step even when an authenticated body is rejected, together with the rejection disposition, the sequence-window change if the sequence is parseable, and initial OPK/session/M changes where applicable. No user receipt is produced for that body.
5. For an accepted type 1 or 2 message, atomically commit DR state, skipped keys and counters, OPK consumption, generation admission, replay bit, durable inbox entry and a unique DECRYPT_COMMITTED intent. For type 3, atomically apply the target's monotone status or durable update intent. Session confirmation and the READY intent are atomic with the same receive.
6. Only after commit may an event be admitted and DELETE scheduled. A failed or full store exposes no plaintext and confirms no commit. DELETE failure cannot roll back the receive. An item that fails crypto may be deleted under the poison policy (Section 9.5), but is never labeled decrypted and never consumes ratchet, OPK or replay state.

A bounded authenticated-index digest cache (at most 4,096 entries per session) may accelerate duplicate rejection. An exact cached duplicate exposes nothing; a cached conflicting digest is rejected without another decryption. Cache eviction never permits reconstructing consumed keys or resetting the message window. DR consumption and the permanent sequence floor, not an unbounded list of every ciphertext, prevent a repeated accepted effect. No accepted cache entry may be created before cryptographic authentication.

## 8.3. Receipt worker and effect capability

Receipt intents are keyed by `(contact,target_seq,receipt_kind)` and READY intents by `(contact,G,SID)`. Create, fulfill, suppress and map each intent to an outbox entry uniquely and durably. For a receipt intent, the worker selects the current encryption-capable active session, or creates a higher one when necessary; it never encrypts in a draining slot. A READY intent is fulfilled only on its own SID and is suppressed once that SID is no longer active (Section 6.4). It allocates one fresh message_seq/MK and commits state, outbox entry and intent mapping together. A crash before commit leaves the intent; a crash after commit retries its ciphertext. Outbox cleanup cannot authorize recreating a completed intent.

Accepted inbox events may be offered to a handler more than once after restart; neither exactly-once handler invocation nor exactly-once network delivery is promised. Application effect identity is the canonical message key from Section 5.3. APPLICATION_COMMITTED is permitted only if:

- the effect and its effect record/receipt intent commit in one atomic transaction; or
- an external adapter provides durable idempotency and a queryable outcome under that key.

Merely scheduling a notification, payment or file write does not qualify. Without such a capability, that receipt type MUST NOT be sent.

An ambiguous external effect stays UNKNOWN with its durable intent. Do not report it as cancelled or not applied because of a timeout, and do not retry it under a new key. A non-idempotent, unqueryable effect cannot be recovered automatically as exactly-once. Retain or quarantine the record, and stop new admission if its finite budget fills. Success in a model or test does not change an external service's semantics.

## 8.4. Exposure fencing and bounded record GC

Immediately before releasing plaintext or admitting an effect, recheck current contact trust, CID, owner fence and storage integrity under serialization. A queued event from revoked, closed or replaced trust is suppressed or retained, never released by a stale callback. Ordinary session retirement alone does not revoke healthy accepted inbox work and MUST NOT undo its receive step. Permission checks do not retroactively recall plaintext already released.

A completed inbox or effect record may be removed only when its seq is below the incoming replay floor, no pending handler, receipt or unknown effect depends on it, and the owner has durably marked it terminal. A later callback without the required record is a no-op, not permission to replay the effect. Pending and UNKNOWN records are never dropped merely because the window advanced. Application history storage and retention are separate, and do not assure that plaintext is securely erased.

Send-status metadata is kept for up to 30 days from the original send, or longer while a live request or effect depends on it within finite admission. Explicit expiration ends receipt reconciliation; a later authenticated receipt becomes UNKNOWN_TARGET. Do not reconstruct an old send or regress known application status. Keep receipt-intent completion while its target or inbox entry depends on it; duplicate crypto input may be deleted without creating another receipt.

## 8.5. Restart and ambiguous outcomes

Before restarting any worker, validate schema, compatibility pins, owner fence, shared prekey allocators, trust records and lease time, then reconcile incomplete transactions. A supported restart preserves active DR state, M and masks, message and RPC sequences, replay windows, uncertain outcomes and immutable outboxes. A process or OS reboot alone does not require replacing a healthy IK.

| Recovery observation | Required disposition |
|---|---|
| Advanced DR and matching outbox committed | Retry exact bytes under persisted age/attempt/permission; no new encryption. |
| Send transaction definitely absent | Discard volatile candidate; recompute only under current state and original logical submission. |
| Receive committed, DELETE missing | Keep inbox/rejection/effect/receipt facts; retry owner cleanup if eligible. |
| Intent pending, encrypted outbox absent | Allocate once under current active state; if mapping committed reuse it. |
| Committed response after local cancel/exhaustion | Reconcile retained facts only; scheduler stays stopped. |
| Partially present atomic records, unreadable pins or irreconcilable commit | STORAGE_INTEGRITY; block affected owner/contact, no outputs/effects. |
| Lease time uncertain but cryptographic counters intact | Section 9.2 cleanup/rebase, not automatic IK replacement. |

A crash MUST leave either no exposed action or one durable decision with a recoverable mapping. No older snapshot, relay OK, cached bundle or packet may fill in missing shared state. If shared identity or prekey ownership is corrupted, block every affected contact, not only the connection that noticed it.

## 8.6. Storage and erasure boundary

The implementation MUST name its database and version, transaction and fsync policy, supported process-crash and power-loss faults, corruption behaviour, keystore and exclusive-owner mechanism. A successful local commit means durable under that declared model; reporting success before that point is prohibited. This single-authority relay role does not imply replication or consensus.

Protect plaintext and private E2EE state at rest under the endpoint's reviewed storage design. Key deletion covers supported library buffers and logical storage, WAL and caches, not only the primary row. Document unavoidable GC, copy-on-write, flash, swap and crash-dump limitations; guaranteed physical erasure of every OS, SSD or backup copy is not claimed. No real key or user data is allowed in test fixtures. Private E2EE material MUST NOT enter logs, metrics or traces, including transport-keylogging paths.

# 9. Time, finite resources and admission

## 9.1. Selected baseline time model

Counter nonreuse and replay safety rely on durable monotone protocol counters, not timestamps. Time controls leases, old-state retention, retries, TTLs and rates.

This baseline assumes that the trusted OS or operator supplies UTC with at most 300 seconds of error and a suspend-inclusive monotone elapsed-time source within each boot. It does not tolerate a malicious privileged clock or rollback of a whole-store snapshot. This profile neither requires nor claims a hardware anchor, cross-reboot monotone counter or external time service. Implementation manifests name and test their actual OS APIs; unsupported platforms cannot claim this lease contract.

Persist the latest accepted lease-time high-water mark H with the state. On owner startup, after validating time, capture UTC0 and the suspend-inclusive elapsed reading E0. Within that boot and process run, lease_now is at least `max(H, current UTC, UTC0 + current_elapsed − E0)`. Persist H before time-dependent admission. Across a reboot, validate UTC against H: a regression of more than 300 seconds, an unavailable clock or a violated monotone source enters TIME_UNCERTAIN. Forward jumps conservatively expire work. Small backward steps never reduce H or extend an existing deadline. A timezone change MUST NOT alter UTC ages. An ordinary valid reboot recomputes ages without resetting creation times.

For a long lease L >= 1 hour, use deadline=`creation_lease_time + L − 600 seconds`, which conservatively budgets the clock-error margin of both endpoints. This applies to key, drain, bundle-use, journal/TTL and seven- and 30-day schedules. Stated lifetimes are maximum nominal budgets, not a promise to retain until the last second. Short attempt timers and rate windows use monotone elapsed durations without this deduction. The ten-minute FETCH_BUNDLE and two-minute POLL deadlines and the one-hour pending-setup threshold use their fixed elapsed durations; across an uncertain reboot they may stop early but are never extended. Original deadlines are immutable.

## 9.2. Clock uncertainty is not cryptographic rollback recovery

On TIME_UNCERTAIN, stop time-dependent network emission, new key publication and reservation, and old-state decryption until maintenance is committed. In addition:

- erase expired or unageable private prekeys, skipped keys and draining slots, and close unseen-lower admission windows;
- stop unageable RPC and receipt schedules with UNCONFIRMED outcomes;
- keep active DR state and counters, replay floors, key-ID allocators and ownership facts if their integrity is independently intact.

Payload expiration cannot make a journal sequence fresh; retain its digest/exclusion slot or the `ack_floor`.

After the OS or operator restores the trusted-time assumption, an explicit local rebase may open a fresh lease epoch for *new* work. It cannot revive any erased key, old drain window, old deadline or stopped request. Refresh publications with fresh IDs and create a higher generation if needed; existing healthy active state may continue under new leases. A clock correction alone requires no bilateral identity reset. Preserve uncertainty about already emitted work.

If instead the database was rolled back, cloned or lost, or cannot establish counter or prekey ownership, a time rebase is insufficient and Section 11 recovery applies. H stored in the same database cannot detect restoration of H together with that database. Universal snapshot-rollback detection, resilience against a clock attacker and physically secure erasure are outside baseline assurance; persisting a timestamp does not provide them.

## 9.3. Endpoint bounds

The following are simultaneous hard upper bounds. Implementations may disclose smaller admission budgets but cannot evict required live safety records to fit them. Allocated and process bytes, provider overhead and physical disk sizing MUST be measured separately; the logical payload bounds make no claim about RSS or fsync.

| Resource | Maximum / rule |
|---|---|
| Verified contacts | 100 per local identity; four retained ratchets/contact, one encrypting. |
| Skipped keys | 1,000/ratchet; max seven-day lease from storage, nonrefreshing. |
| Incoming semantic replay | 4,096 positions/contact, constant-size bitmap and high water. |
| Authenticated-index cache | At most 4,096 entries/ratchet; not the source of permanent replay admission. |
| Live private prekeys | Four SPKs and 4,096 OPKs/identity; expiry and consumed deletion apply. |
| Immutable outbox | 4,096 envelopes and 64 MiB envelope bytes/identity; reserve 128 slots and 1 MiB for READY/receipts. |
| Unprocessed inbox/effect records | 4,096 and 64 MiB body bytes/identity; UNKNOWN records cannot be evicted. |
| Receipt/READY intents | 8,192 and 4 MiB serialized logical bytes/identity. |
| Send-status targets | 65,536 and 8 MiB logical metadata/identity; age/active dependencies apply. |
| Normal RPC records | 64 and 4 MiB immutable request/response bytes/account epoch; at most eight unresolved executing requests, one POLL. |
| E2EE mutable metadata | 256 MiB logical serialized bytes/identity excluding separately bounded body/outbox payloads. |
| Pending crypto workers | Four/identity; bounded immutable input queue of 64 envelopes and 4 MiB. |
| Poison diagnostics | 1,024 records and 1 MiB/identity; at most 256 diagnostic bytes/item. |
| CID admission/exclusion records | 256/identity total, active plus retired; reserve on initial trust admission. Closure marks the existing record in place; each retired CID stays untrusted while the IK is used. |

Every required logical object MUST belong to a stated bucket, and alias or reference storage MUST be charged once under a pinned schema, not ignored. Provider, queue and allocator caps, transaction/WAL caps and global process caps are mandatory manifest fields. If a worst-case combination exceeds a byte bucket before its count ceiling, admission stops early. Limits are ceilings, not targets. Each implementation-manifest budget names its exact value and unit, scope, owner, enforcement point, failure disposition and evidence. Zero never means unlimited. Shrinking a budget below protected occupancy stops new admission; it does not authorize deleting required records or refreshing an outstanding request's age.

A per-contact crypto-work token bucket allows at most four candidate receives per second with burst eight; the identity-wide bucket allows 32 per second with burst 64. These buckets bound admitted E2EE attempts, not wire bandwidth. Together with the four workers and the Section 3.5 caps, they prevent unbounded peer-directed KDF scheduling. Smaller measured limits may apply. Rate-limited packets wait under the finite queue and poison policy; they do not reset trust or silently mutate replay state.

## 9.4. Relay bounds and control headroom

| Resource | Maximum / controlling rule |
|---|---|
| Mailbox/recipient | 1,000 envelopes and 32 MiB exact envelope bytes. |
| Per sender in one recipient mailbox | 100 envelopes and 4 MiB; both limits. |
| Active mailbox TTL | Seven-day lease from first STORE; duplicates do not refresh. |
| STORE-index tombstones | Eight-day lease after first STORE; retain after DELETE/TTL; <=131,072 records and 16 MiB logical bytes/account. |
| Unissued OPKs | 1,000/IK; issued keys never returned; high-water counters survive pool/journal GC. |
| Journal | 64 slots and 4 MiB request/response bytes/account; at most 48 data slots, so at least 16 remain for control; at least 1 MiB of that byte budget reserved for control. |
| STORE rate | 60 requests/rolling minute/account, including retries. |
| FETCH_BUNDLE rate | 60 requests/rolling minute/requester, also <=3 new reservations/hour per requester-recipient pair and <=60/hour per recipient. |
| PUBLISH rate | Six requests/rolling minute/account; retries count. |
| POLL/DELETE/WITHDRAW rate | Combined 120 requests/rolling minute/account. |
| ACK_FLOOR rate | Separate reserved 60 requests/rolling minute/account; coalesce floors. |
| Executing work | Eight normal logical requests/account, one unresolved POLL; bounded per-service pools/queues. |
| Enrolled accounts | <=10,000 per authoritative instance; actual admission may be much lower. |
| Owner/IK exclusion ledger | <=100,000 records and 64 MiB logical bytes/instance, including active/retired IKs and nonreused owner-epoch allocation records. |

The operator MUST reserve control journal, transaction and expiry capacity before admitting data or accounts, and enforce finite global byte, count, worker and storage headroom. Journal, data or tombstone pressure returns QUOTA for new data; it does not delete required unexpired exclusion records. DELETE, `ACK_FLOOR`, WITHDRAW and expiry have separate bounded scheduling and headroom. The 64-slot limit and permanent floor exclusion keep journal storage bounded instead of growing an unbounded observation history.

A successful DELETE or expiry leaves the STORE-index digest and first-STORE age in place until the tombstone deadline. A matching duplicate STORE returns OK without restoring the item; conflicting bytes return CONFLICT. At tombstone capacity, reject new STORE requests before mutation; owner cleanup remains admitted from the control reserve. After tombstone expiry, no permanent STORE-index history is promised: compliant clients never resubmit an old logical STORE under a new sequence, and an old sequence remains excluded by its slot or the floor. Endpoint replay protection is independent.

Physical disk failure, exhausted database/WAL headroom or storage quarantine may stop all writes; safety takes precedence. A relay cannot classify encrypted receipts as control traffic from content_type, so this reserve does not guarantee E2EE receipt admission into an already full mailbox. Mailbox draining, endpoint capacity, prekey supply and fair successful requests remain prerequisites for progress. No numerical latency, throughput or sustained-rate SLO is defined.

Reserve an exclusion-ledger record when an IK or owner epoch is first admitted. Retirement or revocation marks that existing record in place and MUST remain possible at the count limit. A full ledger stops new enrollment and owner replacement rather than forgetting retired identities or prekey-ID marks; a stopped replacement does not prevent disabling its compromised predecessor. Existing healthy identities keep constant-size counters for ordinary publication and renewal.

Arrival ordinals and request sequences never wrap. Exhaustion stops the corresponding new work until an explicitly provisioned, never-reused owner epoch or service partition is established. That transition cannot relabel or re-emit old RPCs or assert that their unknown effects are absent, and it does not reset the contact's E2EE counters or trust.

## 9.5. Poison input and bounded loss

For a stored item, permit at most three local crypto-processing attempts within its retained diagnostic lifetime, each within the per-attempt work limits. After repeated authentication failure, stale or retired-session failure, or public malformation, the endpoint may DELETE the item without a receipt and report bounded loss or UNCONFIRMED status. Diagnostics store only the index, digest, a bounded error class and at most 256 diagnostic bytes per item, never keys or plaintext. An authenticated rejected body goes through its durable rejected-disposition transaction before DELETE.

At the diagnostic budget, stop recording diagnostics or perform declared owner cleanup; do not insert an accepted replay entry to suppress unauthenticated data. A deletion under this policy is not evidence that a message was never valid: delayed traffic may fail after legitimate retention cuts. Fair round scheduling still lets other eligible items be observed when one item cannot be processed or deleted. Global storage failure or a malicious relay can still prevent progress.

# 10. Binding B1-R2 and exact size budget

## 10.1. Selected carrier and protocol-local assignment

B1-R2 is a TCP binding: three-flight Noise_XX_25519_ChaChaPoly_SHA256, magic DGP1, transport version 1, a 40-byte frame header and ChaCha20-Poly1305 post-handshake frames. Sections 10.2 and 10.5–10.8 define its framing, handshake, key epochs, replay and control messages. The transport version and ASCII labels below are wire constants, not document names or the application version. Hop encryption protects the endpoint–relay connection; it is not endpoint-to-endpoint E2EE.

> [!NOTE]
> **Relationship to the DGProto v1 transport**
> B1-R2 reuses the frame layout, handshake pattern, key schedule and control messages of the DGProto v1 transport, but uses its own Noise prologue (Section 10.6). DGProto v1 peers and B1-R2 peers therefore cannot complete a handshake with each other. This section is the complete B1-R2 definition; implementers do not need the v1 transport specification.

B1-R2 assigns the SessionClose reason field, fixes control and rekey processing rules, and uses its own Noise prologue. No other prologue is an alternative parse or a negotiated fallback.

**B1-R2 assigns Stream ID=0x0002 and App Msg Type=0x02 on a dedicated DG2 service.** This assignment is *protocol-local* to this binding; it claims no allocation in an external or global v1 registry and describes no existing deployed configuration. The listener and account partition MUST NOT multiplex an incompatible legacy application on that tuple. An operator facing a collision uses a separate service endpoint or partition; it cannot silently choose different bytes for B1-R2. Trusted configuration pins the server transport identity, ContextID, B1-R2 and profile 2.

B1-R2 uses complete-field TLV alignment and authenticates both the original header and the clear frame padding. Value-only TLV alignment and header-only AAD on padded frames are incompatible with this binding, and no legacy fallback is allowed. Source code and historical implementations are not an alternative authority for these rules, nor evidence of conformance.

## 10.2. Exact encapsulation and parser boundary

One EncryptedData frame (type 0x03) contains exactly one mandatory application TLV:

```text
EncryptedDataPayload = LE16(0x0002) || 0x02 || 0x00
                    || 0x01 || RPC_len:u16 || RPC:bytes[RPC_len]
                    || TLV_padding:bytes[p]
p = (4 - ((3 + RPC_len) mod 4)) mod 4
align4(x) = 4 * ceil(x/4)
```

TLV Type=1 carries binary RPC, not text. A missing, additional or duplicate TLV, another tuple, a nonzero EncryptedData reserved byte, a bad extent or trailing bytes reject the entire application payload. Senders write TLV padding as zeros; receivers accept opaque nonzero alignment bytes after hop authentication. Alignment bytes never enter RPC comparison or E2EE AD. Do not normalize any received bytes before transport authentication. There is no trial parse or value-only fallback.

A complete data frame is `header40 || hop_ciphertext || tag16 || frame_pad`, with total size `40 + PayloadLength + 16 + PadLength` <= 65,535 and no outer length prefix. Frame PadLength is 0–255. The hop AEAD AAD is exactly the original 40 header bytes followed by the original clear trailing frame-padding bytes. Receivers ignore the semantics of reserved outer-header fields but authenticate their bytes unchanged. The padding-present flag and PadLength MUST agree. Clear random frame padding is distinct from encrypted TLV alignment padding.

Validate the header and total bound before allocation, then authenticate and validate the full single-field extent before dispatch. Generic TLV parser success is not DG2 admission. Safe byte access is required unless measured unsafe/alignment preconditions are explicitly proved; an encoded length that is a multiple of four does not prove that the input buffer address is aligned. Parser or runtime failures may close the hop but cannot reset a committed E2EE session.

## 10.3. Exact maximum sizes

```text
P_max          = 11 + 32768                = 32779
CBC_max        = 16 * (floor(P_max/16) + 1) = 32784
C_max          = CBC_max + 32              = 32816
REGULAR_max    = 215 + C_max               = 33031
INIT_max       = 396 + C_max               = 33212
STORE_RPC      = 59 + INIT_max             = 33271
POLL_OK_RPC    = 59 + 1 + INIT_max         = 33272
TLV_max        = align4(3 + POLL_OK_RPC)   = 33276
L4_max         = 4 + TLV_max               = 33280
data_frame_max = 40 + L4_max + 16 + 255    = 33591
```

The largest EncryptedData frame is 33,336 bytes without clear frame padding and 33,591 bytes with 255 padding bytes. These are application-data maxima, not the generic transport ceiling: a control frame may be larger, up to the 65,535-byte complete-frame bound in Section 10.5. POLL_OK has one alignment byte; STORE has two. A valid empty body yields C_len=48, REGULAR=263 and INIT=444. A receipt's nine-byte body yields a 20-byte plaintext, C_len=64 and REGULAR=279. All 256 frame-padding lengths fit with the maximum application body. Encoders MUST preflight the actual complete size, including the chosen padding, before enqueue.

## 10.4. Codec examples and operational adapter rules

These TLV examples are synthetic generic-codec literals, not authenticated frames. Type 0x2a is a generic fixture value, not DG2's assigned Type 1.

| ID | Hex bytes | Result |
|---|---|---|
| B-T0 | 2a000000 | Empty value; extent four. |
| B-T1 | 2a0100aa | One value byte; no pad. |
| B-T2 | 2a02001020000000 | Two value bytes; three pad bytes. |
| B-T3 | 2a03001020300000 | Three value bytes; two pad bytes. |
| B-TP | 2a03001020307f80 | Accept opaque receive pad; canonical send uses zeros. |
| B-NP | 2a030010203000 | Reject missing pad/value-only extent. |
| B-NT | 2a0300102030000000 | Reject trailing byte. |

Product fixtures MUST also test the fixed tuple, single TLV, RPC magic and header, outer padded AAD, partial TCP reads, multiple frames per read and truncated frames. Independent decoder tests and tests over an actual transport remain required.

Snapshot buffers before validation and queue handoffs. Offload bounded crypto and database work from serial transport readers. An adapter MUST contain a nonfatal single-packet rejection rather than turn it into an accidental handler-fatal error or panic. A transport-fatal framing or authentication failure may close the hop without resetting committed E2EE state. Queue acceptance or a local write wait is not a remote commit.

The last serialized application authorization before queue handoff is the emission boundary. Revocation prevents later authorized submissions, but already handed-off bytes may still be written; cancellation and Close cannot guarantee retraction. Stronger write-time revocation needs separately tested transport support. A reconnect or rekey uses a new hop sequence but the same immutable logical RPC and E2EE bytes; it creates no new MK, semantic sequence, owner epoch or contact verification.

## 10.5. Transport header and stream framing

TCP is a byte stream: a read may contain part of one frame or several frames. Read the fixed header first, validate lengths with checked arithmetic, then read exactly the declared remainder. All transport multibyte integers are little-endian. Serialize complete outgoing frames through one connection writer; concurrent writes MUST NOT interleave frame bytes. An incomplete or ambiguous stream write requires closing that hop, not appending a reconstructed frame to it. Retry logical RPCs only on a fresh authenticated hop, under Section 7.7.

| Field | Offset / bytes | Required interpretation |
|---|---|---|
| Magic | 0 / 4 | Exact octets 44 47 50 31 (ASCII DGP1). |
| Version | 4 / 1 | 0x01. |
| Flags | 5 / 1 | Bit 1 indicates frame padding; all reserved bits zero on send, ignored on receive. |
| MsgType | 6 / 1 | Message registry in Section 10.8. |
| Reserved | 7 / 1 | Zero on send; preserve received octet in encrypted-frame AAD. |
| SessionID | 8 / 16 | Zero during handshake; derived identifier after completion. Not the E2EE SID. |
| Sequence | 24 / 8 | Zero during handshake; nonzero, increasing per sending direction/key epoch afterward. |
| PayloadLength | 32 / 4 | Payload bytes only, excluding outer tag and trailing frame padding. |
| PadLength | 36 / 1 | Clear trailing frame-padding length, 0–255. |
| Reserved | 37 / 3 | Zero on send; preserve received octets in encrypted-frame AAD. |

Handshake frames (types 0x01 and 0x02) are `40 + PayloadLength + PadLength` bytes, with no outer tag; Noise authenticates its own handshake messages. Every post-handshake frame is `40 + PayloadLength + 16 + PadLength` bytes. In either case the complete frame is at most 65,535 bytes, and no length is prepended outside the header. Reject an invalid magic, version, state/type, SessionID, sequence or extent before application dispatch, and never allocate from an unchecked u32 length.

Flags bit 1 is set if and only if PadLength is nonzero. Bits 0 and 2 and all other flag bits are reserved; they do not negotiate features. Reserved header fields are not normalized before authentication. In encrypted frames the 16-byte tag follows the ciphertext, and clear frame padding follows the tag and is included in the AAD exactly as in Section 10.2. Sequence reuse under a traffic key is forbidden.

Frame-padding bytes MUST be generated by a CSPRNG, not deliberately zero-filled. Senders SHOULD use a fixed length-bucketing policy, within the 255-byte and total-frame bounds, rather than arbitrary padding lengths derived directly from plaintext size. Padding does not hide explicit lengths or guarantee traffic-analysis resistance. Frame padding is separate from the zero-filled encrypted TLV alignment.

## 10.6. Noise handshake and initial traffic keys

B1-R2 uses the Noise Protocol Framework, revision 34 (2018-07-11), with exactly Noise_XX_25519_ChaChaPoly_SHA256 [8]. The XX token sequences, in flight order, are `e`; `e, ee, s, es`; and `s, se`.

Before the first token, MixHash the 17-byte prologue ASCII("DGProto 2.0 B1-R2") without a terminator, length prefix or newline. Its exact hex is `44 47 50 72 6f 74 6f 20 32 2e 30 20 42 31 2d 52 32`. Supply this prologue exactly once through the Noise initialization API; do not prepend it to a flight or hash it again if the library already processes it. B1-R2 does not use the earlier five-byte DGPv1 prologue; the distinct transcript prevents an implicit B1-R1 handshake fallback. Other patterns, resumption, 0-RTT and early application data are not supported. Use a maintained, reviewed Noise implementation. Transport static-key acceptance is the explicit trusted configuration and authorization of Sections 2.3 and 10.1; it is not proof of E2EE person verification.

| Flight | Frame type / payload bytes | Exact payload |
|---|---|---|
| Client to relay, message 1 | 0x01 / 36 | Pattern 0x01, three zero reserved bytes, Noise e:bytes32; no Noise payload or static key. |
| Relay to client, message 2 | 0x02 / 96 | Noise e:bytes32, encrypted static key:bytes48, empty-payload authentication tag:bytes16. |
| Client to relay, message 3 | 0x02 / 64 | Encrypted static key:bytes48, empty-payload authentication tag:bytes16. |

Direction and handshake state distinguish the two type-0x02 shapes. Both peers use zero header SessionID and Sequence throughout these flights, and no flight has an outer transport AEAD tag.

After authenticating flight 2, the client MUST compare the received relay static key with its trusted configured pin before producing flight 3 or releasing any application bytes. A mismatch closes the hop, without TOFU or pin replacement. The relay MUST validate the client static key and its current account/owner mapping before accepting post-handshake operations; successful Noise processing alone does not supply this authorization.

The client enters the encrypted state only after the pin check and production of flight 3; the relay only after authenticating flight 3 and accepting the client's transport identity. No application data is accepted before that transition. Handshake outer headers and clear padding are not authenticated as transport AAD, because Noise authenticates its own messages; their exact framing constraints still apply.

```text
channel_binding = final 32-byte Noise handshake hash after flight 3
SessionID = SHA256(ASCII("DGPv1 SessionID") || channel_binding)[0:16]
(k1, k2) = standard Noise Split()
traffic_nonce = 0x00000000 || LE64(Sequence)
```

No additional HKDF or label is applied to the Split outputs: k1 is the client-to-relay key and k2 the relay-to-client key. Post-handshake encryption is ChaCha20-Poly1305 with that direction's key, the 12-byte nonce above and the exact AAD of Section 10.2. The first post-handshake Sequence is 1 in each direction. The binding supplies this nonce explicitly; using a Noise CipherState's default zero starting nonce without setting Sequence is not conformant. A hop restart requires a fresh handshake, not restoration of an old traffic key with reset sequence state. Transport sequences, key epochs and SessionID are not E2EE AD, message_seq or generation state, and a new hop cannot reset those application facts.

## 10.7. Directional rekey and hop replay

Each sending direction starts in epoch 1. Before further application traffic, a sender MUST initiate the next epoch when either 2^32 frames have been sent in the current epoch or ten minutes have elapsed since that epoch began. Concurrent send workers crossing one boundary produce exactly one RekeyInit.

RekeyInit (type 0x08) is encrypted with the old current key K. Its plaintext is exactly `E:u32 || KeyConfirm:bytes32`, where E is the next directional epoch:

```text
KeyConfirm = HMAC-SHA256(K, ASCII("DGPv1 Rekey Confirm") || LE32(E))
K_next     = HMAC-SHA256(K, ASCII("DGPv1 Rekey Send Key"))
```

**Sender.** The direction's single serialized writer queues RekeyInit after all previously authorized old-epoch frames, as the final old-epoch frame. It then installs K_next, advances to E and resets that direction's sequence to 1. A worker MUST NOT enqueue another old-key frame after that boundary. If the writer cannot establish this ordering or an unambiguous local transition, it closes the hop and performs a fresh handshake before further emission.

**Receiver.** The receiver authenticates RekeyInit with the current key, requires E = current epoch + 1, checks KeyConfirm in constant time, then installs K_next and resets the new epoch's replay window. New-epoch frames begin at sequence 1. This HMAC derivation is not Noise's REKEY function. The ASCII label "DGPv1 Rekey Receive Key" is reserved for an API output; it MUST NOT replace the send label when deriving either peer's next wire traffic key.

**Previous-key grace.** A receiver MAY retain the immediately previous receive key for delayed non-rekey frames until 2,048 newly accepted current-epoch frames or 30 elapsed seconds from installation, whichever comes first. Duplicates and failed authentication neither renew the deadline nor count as accepted frames. Expiry erases the previous key and its replay window. Installing another epoch discards any older grace state, so at most two receive keys are retained, each with a separate replay window.

**Rejection.** A RekeyInit authenticated under the previous key is rejected as a duplicate/rollback, even if its sequence was already seen; it is not a generic authentication failure. Reject epoch zero, duplicate, rollback, skipped or future epochs, and any advance beyond the u32 maximum. An invalid confirmation changes no key, epoch, sequence, replay or grace state.

**Hop replay.** For each receive direction and key epoch, keep a 64-bit highest sequence and a fixed-size replay bitmap; a 2,048-position window is RECOMMENDED. After the public frame, session, type and nonzero-sequence checks, authenticate under the applicable current or retained previous key *before* replay classification. A frame that authenticates under neither key is an authentication failure regardless of its claimed sequence. Use only the authenticating key's window; the previous-key RekeyInit rule takes precedence over ordinary duplicate classification. For an authenticated sequence n:

- If n exceeds highest, shift the bounded bitmap by n−highest, discarding old positions (clear it if the shift covers the window), set highest=n and mark n.
- Otherwise, if highest−n is within the window, reject if marked; else mark it.
- Otherwise reject as too old. Authentication failure never advances the window.

Process authentication, replay classification and RekeyInit validation on candidate hop state, and commit a replay mark or epoch transition only after all applicable checks succeed. In particular, a valid outer tag with an invalid KeyConfirm MUST NOT consume a sequence or alter either window. A rejected candidate delivers no application or control event. A repeated previous-key RekeyInit is rejected without installing an epoch or extending grace.

This hop window does not replace the durable E2EE semantic window, the operation journal or the session-generation exclusion rules. Directional HMAC rekey limits old-key retention; it adds no fresh DH entropy and does not heal a compromised current traffic key. Recovery from that compromise requires a fresh authenticated handshake under healthy credentials.

## 10.8. Transport controls and non-extension boundary

| Type | Message | Payload / role |
|---|---|---|
| 0x01 | HandshakeInit | Client flight 1 only, Section 10.6. |
| 0x02 | HandshakeResponse / Finish | Flights 2/3 only, Section 10.6. |
| 0x03 | EncryptedData | Exactly the DG2 application payload in Section 10.2. |
| 0x04 | Ping / Pong | Exactly nine bytes: is_response:u8 (0=ping, 1=pong), nonce:bytes8. |
| 0x05 | SessionClose | close_code:u16, with zero or one reason TLV defined below. |
| 0x06 | Ack | count:u8 in 1–255, then exactly count nonzero sequence:u64 values. |
| 0x07 | Reserved resumption | MUST NOT be sent; not a supported extension. |
| 0x08 | RekeyInit | Exactly 36 bytes; Section 10.7. |
| 0x09 | Error | code:u16 then a strictly valid UTF-8 remainder, possibly empty. |

All supported post-handshake controls use the same directional key, sequence and replay rules as EncryptedData. Unknown types, invalid exact lengths, unsupported field values, invalid UTF-8 or trailing bytes reject the complete control. A syntactically malformed authenticated control closes the hop; it cannot reset committed E2EE, RPC or trust state. Replay and RekeyInit transition failures follow Section 10.7 and produce no control response.

**SessionClose.** The codes are 0=normal, 1=authentication expired, 2=protocol error and 3=idle timeout; other codes are unsupported. The exact forms are:

```text
SessionClose = close_code:u16 || Reason_if_present
Reason_if_present = empty, or
    0x01 || reason_len:u16 || reason:bytes[reason_len] || pad:bytes[p]
p = (4 - ((3 + reason_len) mod 4)) mod 4
```

Type **0x01 is assigned to the UTF-8 reason only within SessionClose**; it is not part of the application RPC TLV namespace. The reason is strictly valid UTF-8; empty is permitted. Senders write alignment padding as zeros; receivers accept opaque alignment bytes after hop authentication. A second TLV, another Type, missing padding or trailing bytes is invalid. The complete payload must fit the actual frame budget of Section 10.5; checking reason_len alone is not sufficient. An absent reason gives a two-byte payload. For example, code 0 with reason `bye` is `00 00 01 03 00 62 79 65 00 00` (ten bytes).

After accepting SessionClose, stop new application handoffs on that hop and close it. Close neither retracts previously handed-off bytes nor proves a remote application outcome. The reason is informational and cannot authorize a retry, trust change or session reset. This assignment and behaviour belong to B1-R2; the earlier binding left this optional field unspecified.

**Ping/Pong and transport Ack.** An accepted ping produces at most one pong with the same nonce, subject to bounded control admission; a pong MUST NOT generate another pong. Generating transport `Ack` messages is optional, and one `Ack` may batch observations of accepted EncryptedData frames. Do not acknowledge Ack, Pong, Error, SessionClose or RekeyInit frames, and do not respond to an `Ack` with any automatic control. Lost control admission may suppress a pong or `Ack`; it cannot consume unbounded queue or storage capacity.

Sequence numbers in a transport `Ack` carry no key epoch and may repeat after directional rekey. They are informational hop observations only: a receiver MUST NOT infer an epoch or an E2EE or relay outcome from them. The acknowledged values MUST NOT release an RPC or outbox record, advance `ACK_FLOOR`, retire a traffic key or change any replay window. Processing the `Ack` frame itself still marks that frame's own Sequence under Section 10.7. RPC results and endpoint receipts supply their own, separate commit evidence.

**Error.** Any u16 code is an informational local transport code; no shared Error-code registry or mandatory response is defined. The entire remainder is UTF-8 (not a TLV or length-prefixed value) and may contain NUL. Its maximum is 65,477 bytes without frame padding, reduced by the actual padding. Diagnostic and UI output MUST escape control characters and obey the implementation's finite logging limits. Error MUST NOT trigger an automatic Error or override a durable application result.

Neither a control message nor a reconnect authorizes an identity, E2EE session or operation reset. Reserved header bits and unknown TLVs do not enable QUIC, obfuscation, additional handshake patterns, resumption or alternative application tuples.

# 11. Errors, trust changes and compatibility

## 11.1. Local error and disposition table

The categories below are local; they are not RPC statuses or transport Error codes. Peer-facing errors MUST NOT reveal HMAC, padding, private-prekey or ratchet sub-errors. Report bounded public diagnostics without secrets. Errors never authorize plaintext fallback.

| Local category | Mandatory disposition |
|---|---|
| TRUST_REQUIRED | Suspend contact content; independently verify person/IK/context/CID/profile; do not overwrite trust from network. |
| PREKEY_UNAVAILABLE | Wait/replenish under quota; no 3DH/no-OPK fallback. Healthy existing session may continue. |
| MALFORMED / UNSUPPORTED | Reject public grammar before crypto; authenticated body errors consume their DR step as specified. |
| AUTHENTICATION_FAILED | Uniform HMAC/CBC/unpad failure; discard candidate, no plaintext or cryptographic-state change. |
| AUTHENTICATED_REPLAY / STALE_WINDOW | Commit successful receive step with rejected body; no repeated message effect or receipt intent. |
| STALE_GENERATION / RETIRED / CONFLICT | Preserve M/trust/current sessions; no resurrection/reset. |
| RENEWAL_REQUIRED / LIMIT | Fresh higher generation if healthy state/prekeys permit; stop at hard bounds, never wrap or replace IK merely for routine count. |
| STORAGE_FULL / QUOTA | Stop new capacity-dependent admission; keep required safety and unknown-outcome records. |
| TIME_UNCERTAIN | Section 9 lease cleanup/rebase; do not invent elapsed time or automatically rotate healthy IK. |
| STORAGE_INTEGRITY | Fence affected work, reconcile or perform explicit recovery; no old snapshots or network-assisted reset. |
| UNCONFIRMED / UNKNOWN_TARGET | Preserve known status/unknown effect; no inference of nondelivery, no automatic reencrypt under new identifiers. |

The API MUST preserve each error's stage and its retry/fatal mapping. A transport failure can close a hop without closing E2EE. A nonfatal local rejection does not permit a panic, unbounded allocation, work or queueing, or an accidental handler-fatal policy.

## 11.2. Closure and new-CID recovery

An explicit, locally confirmed contact closure retires the CID, blocks all its new encryption, decryption and exposure, stops associated workers and erases its session secrets. Preserve a retired-CID record while the IK is in use; old-CID packets stay untrusted after cleanup. Ordinary slot retirement is different and does not close the contact. There is no unauthenticated RESET or trust-update packet.

To reopen an intentionally closed contact, or to recover from *contact-local* state loss while the shared identity, private-prekey allocator and single-owner integrity are known to be healthy, both people independently verify a fresh CID and the exact trust transcript, retire old-CID work and acknowledge its uncertain outcomes. Both persist old-CID disablement before activating the new record. Healthy IKs may be retained, and other contacts are unaffected. A new CID does not recover history or cancel an earlier external effect. No network message can approve this procedure.

Reserve one CID admission/exclusion record before activating each contact. Closure or revocation marks that existing record in place and MUST remain possible at the limit. At the total active-plus-retired CID budget, stop new trust and recovery admission rather than forgetting a retired CID. Routine session renewal never spends another record. If origin or message counters reach u64 exhaustion, the same explicit new-CID procedure is available; it is not an automatic wrap. Reusing an old CID under the same identity is forbidden.

## 11.3. Compromise, shared-state loss and revocation

Suspected identity compromise, whole-store rollback or clone, a lost shared prekey allocator, or unresolvable exclusive-owner state requires stopping all affected work, retiring the affected local IK, revoking old transport and account authority, erasing secrets and independently notifying contacts. WITHDRAW is best effort, not proof of remote erasure. Provision a fresh local IK and OwnerEpoch and independently reverify each contact. Healthy peers need not replace their own unaffected IKs; they MUST explicitly approve the changed peer and a fresh CID.

The affected endpoint's other contacts are also impacted, because its IK changed. Silent trust migration and directory assertions are not permitted. Remote revocation is not instantaneous: a relay may suppress notices, and already released plaintext or authorized bytes cannot be recalled. Fresh prekeys cannot repair a stolen IK or reconstruct erased messages. Unknown external effects remain unknown across recovery; do not automatically replay them as new-CID actions.

## 11.4. Compatibility registry and evolution

| Identifier | Disposition |
|---|---|
| application version 2 / profile 1 | Reserved; never valid. Never accept in a P2 contact. |
| application version 2 / profile 2 | The classical profile and lifecycle defined here. |
| RPC magic DGR2, version2/profile2 | The bounded sequence/floor RPC contract defined here. |
| Binding B1-R1 | Reserved; never valid. No implicit parse, prologue or control fallback. |
| Binding B1-R2 | Dedicated service, tuple (0x0002,0x02), revision-specific Noise prologue, complete-field TLV alignment and header\|\|clear-pad AAD. |
| Other values/bindings | Unsupported until separately assigned and specified; no negotiation or trial fallback. |

The assignments in this document are protocol-local; it asserts no external registry ownership. The specification publisher maintains this registry and the released artifact identifiers. Operators control ContextID and authorization provisioning, not cryptographic reinterpretation. A matching document revision alone is not a deployment manifest.

**Revisions.** The document carries one integer revision. Every normative change increments it and is recorded in Section 13.4; editorial changes that alter no bytes, state transitions or accepted/rejected sets may be made without an increment. The rules for normative changes depend on the status:

| Status | Normative change |
|---|---|
| Draft (current) | Corrected in place under the same identifiers. No deployment carrying real user data may use a Draft revision, so no migration is required. |
| Stable | Only editorial changes in place. An incompatible security, wire or state correction requires a new explicit profile/version or binding revision and a migration decision; reusing profile 2 for different behaviour is forbidden. |

The status changes from Draft to Stable when a reference endpoint and relay pass the Section 12 gates and the published test vectors are fixed in the revision that becomes Stable.

Transport framing remains DGP1 version 1; trusted service configuration and the exact Noise prologue of Section 10.6 select B1-R2. The tuple is not a binding-negotiation channel. A binding-only move MUST preserve the exact committed P2 envelopes and RPCs, owner authority, sequences and uncertain outcomes; it does not permit reencrypting old work or relabeling an incompatible store.

## 11.5. Security and privacy limitations

Classical X3DH/DR does not provide post-quantum protection. Forward secrecy depends on actual key deletion and an uncompromised RNG and storage; retained skipped keys, prekeys, WAL and plaintext expose their corresponding history. An honest fresh DH contribution after repair may help against passive compromise, but an ongoing active MitM, a stolen IK or persistent endpoint compromise is not automatically healed. One-way traffic without responses does not produce regular DH healing. [4] [5]

The relay sees IK routing, ContextID and CID, generations and SIDs, setup and prekey references, DH/PN/N, lengths, timing, IP addresses and relationships. Clear transport padding does not hide its explicit payload and pad lengths or the fixed magic. The protocol provides no anonymity, sealed sender, censorship resistance, hidden social graph or guaranteed deniability of the whole application. Quotas cannot prevent a hostile relay from censoring or indefinitely retaining every ciphertext and metadata item.

Private IK/SPK/OPK/EK/DH, SK/RK/CK/MK, signature Z, derived IV/subkeys and plaintext MUST NOT enter logs, metrics, traces, dumps or production fixtures. E2EE secrets MUST NOT be exported via transport keylogging. Production builds MUST exclude hop keylogging and secret-bearing crash dumps. E2EE does not make captured hop traffic safe.

# 12. Conformance and acceptance scenarios

## 12.1. Conformance requirements

Conformance requires every applicable normative rule in Sections 1–12, including exact grammars, bounds, declarative state transitions and the B1-R2 semantics. Any departure from a SHOULD requires the recorded justification specified in Section 1.1. Independent cryptographic, session and storage review, remediation and cross-implementation interoperability MUST precede use with real user data. Codec checks, bounded models and primitive vectors are insufficient by themselves.

| Gate | Required independent/implementation evidence |
|---|---|
| C01 | RFC 7748 and exact XEd25519 acceptance-set/provider vectors; canonical/noncanonical, low-order, all-zero DH, Z and conversion behavior. |
| C02 | Full four-DH SK and DR RK/CK/MK/CBC/HMAC intermediates; exact P2 labels, Setup/AD lengths/order, cross-context/profile/CID/role negatives. |
| C03 | Offline INIT N=0/31, first response freshness, READY and authenticated rejected bodies; INIT-to-REGULAR and one-time OPK use. |
| C04 | Simultaneous initiations/renewals, total order/masks/four slots, stale descriptor and OPK races; no lower-generation reactivation. |
| C05 | DR and semantic-window reordering/duplicates/expiry, limits, renewal, skipped-key preservation on failure, bounded KDF and no sequence reuse. |
| C06 | Every AD/header/setup/cipher/HMAC mutation, invalid DH and roles; no plaintext/state change on crypto failure, uniform HMAC-before-unpad failure. |
| C07 | Truncation/extra bytes/endian/types/UTF-8/length/PKCS#7 negatives; authenticated invalid-body disposition and bounded parsing. |
| C08 | Body 0/32768/32769, all block boundaries and all 256 transport padding lengths; fixed tuple, TLV extent, B1-R2 AAD, 33,591-byte application-data maximum versus 65,535-byte transport ceiling; strict controls, pin order, sequence/rekey/grace and B1-R1 rejection. |
| C09 | Actual crash/race/ambiguous-commit campaigns at all send/receive/effect/receipt/ACK_FLOOR/DELETE boundaries with supported fsync/fence model. |
| C10 | Relay authorization/reassignment/revocation, journal floor/staging/expiry, stale workers, prekey/tombstone limits, poison/full mailbox and fair-round progress. |
| C11 | Trust UI, closure/new CID, compromise/shared loss, time uncertainty/rebase/suspend/reboot; retired work never regains rights. |
| C12 | Profile-1/old labels/cross-domain/legacy binding rejection; reconnect/rekey preserves core bytes and uncertainty, no plaintext fallback. |
| C13 | Two independently written implementations produce exact packets and agree on rejection/state/effect outcomes with pinned code/dependencies/spec. |
| C14 | Grammar/state fuzzing, CPU/RAM/disk/global admission and secret-safe diagnostics across default/tagged/production builds. |

Every gate requires a record of: the requirement or scenario; fixture inputs and expected result; generator provenance; source, dependency and specification hashes; environment; observed outcome; reviewer; unresolved assumptions; and the rerun command. Missing evidence is NOT RUN, not PASS. A specification decision can be fixed while its implementation gate remains open. Automated self-checks do not constitute independent review.

## 12.2. Mandatory distinguishing traces

Each trace below is an acceptance requirement. Fixture material MUST be synthetic and public-safe. Trace IDs are stable labels for cross-reference, not wire codes.

| Trace | Adversarial event | Required outcome |
|---|---|---|
| T01 | Both create same serial before hearing the peer | Greater raw-origin tuple is the encryption choice; lower slot receives only; no merge. |
| T02 | Deliver lower INIT after a higher generation | Admit only in unseen two-counter window/deadline/capacity; never lower M. |
| T03 | Retire lower slot, replay its INIT | No X3DH/OPK reuse or slot recreation, including after mask GC below floor. |
| T04 | Erase pending slot; late valid READY/REGULAR | No resurrection; explicit uncertainty; a fresh higher generation may proceed. |
| T05 | Lose all confirmation messages | Pending limit/age prevents invalid INIT headers; higher generation does not reencrypt old content. |
| T06 | First authenticated INIT has malformed body | One OPK/session/DR commit and rejection; active responder can send READY, not false user receipt. |
| T07 | First authenticated response body is invalid | DR step commits; initial phase ends cryptographically; no application confirmation. |
| T08 | Prekey authenticates in two concurrent candidates | One successful consumption commit; loser reloads without output. |
| T09 | Hard count hit with receipt intent pending | Last valid receive commits; slot retires; receipt uses higher active session or remains unconfirmed. |
| T10 | Same seq appears in another authenticated session | One semantic disposition/effect; second consumes DR only and is rejected. |
| T11 | Large valid sequence leap over unallocated holes | Constant-time bitmap shift; older gaps become stale, not declared committed/cancelled. |
| T12 | Replay after old semantic bitmap positions were dropped | Below-floor rejection; no old effect reenactment. |
| T13 | Pending external effect falls below replay floor | Keep UNKNOWN record; no presumed cancellation or new-key retry. |
| T14 | Crash after send commit but before write | Exact ciphertext/seq retry, one ratchet advancement; original deadline/attempt facts survive. |
| T15 | Crash after receive before receipt/DELETE | No repeated effect; fulfill one intent mapping, idempotent owner cleanup. |
| T16 | Stale send/receive/expiry/trust callback wins late | Fence/version compare rejects commit/exposure; discard candidate. |
| T17 | Bad HMAC for a saved skipped key | Live key preserved, no alternative-key fallthrough; later valid input consumes once. |
| T18 | Full data journal and full mailbox | New STORE may fail; ACK_FLOOR/control/expiry retain reserved bounded progress when storage healthy. |
| T19 | ACK_FLOOR crosses allocated hole and uncertain STORE | Stops later uncommitted stale effect; prior possible commit remains uncertain, not undone. |
| T20 | Journal payload expires, then retry same seq | EXPIRED/exclusion, never a fresh reservation/STORE/POLL effect. |
| T21 | Lost FETCH_BUNDLE result and journal retry | Same exact reservation; no OPK returned; new-ID publication exclusion survives withdrawal/GC. |
| T22 | Reuse old numeric OPK ID after all public rows removed | CONFLICT/high-water exclusion; new higher IDs remain admissible with fresh keys. |
| T23 | Poison item repeatedly returned; continuous new arrivals | Snapshot round cut prevents starvation of other continuously eligible items within selection bound. |
| T24 | POLL completed; DELETE/lost reply; cached POLL repeats | Same old observation, no new cursor/event/effect; fresh logical POLL needed. |
| T25 | Revoke or reassign account before cached result release | No old owner/forbidden sender ciphertext; cleanup still authorized for current owner. |
| T26 | Sleep/reboot offline, UTC step backwards/forwards | Immutable ages and baseline assumption checked; time-only cleanup does not rotate healthy IK. |
| T27 | Whole DB+H snapshot restored or clone detected | No timestamp-based safety claim; block and explicit shared-state/IK recovery. |
| T28 | Unknown IK/profile/CID or forged high generation | Reject without erasing healthy trust/M; no fallback, no unauthenticated eviction. |
| T29 | User closes contact then old accepted event executes | Current trust/fence suppresses exposure; no old-CID revival. |
| T30 | Endpoint compensation/retry after unqueryable effect timeout | UNKNOWN retained; no false APPLICATION_COMMITTED or assumption of no effect. |
| T31 | Valid Noise flight 2 presents an unpinned relay key | Close before flight 3/application release; no TOFU or pin replacement. |
| T32 | SessionClose has an unknown/duplicate reason TLV, bad UTF-8 or missing pad | Reject complete control and close hop; no E2EE/RPC reset. |
| T33 | Pong, transport Ack or Error arrives; Ack numbers collide across rekey | No control feedback loop or commit inference; durable application records unchanged. |
| T34 | Authenticated RekeyInit has bad confirmation, duplicate or future epoch | No epoch/window/sequence/grace mutation or dispatched event. |
| T35 | Old-key writer races RekeyInit; previous-key grace expires | One serialized final old-key frame; no later old-key enqueue; erase expired previous key/window. |
| T36 | Another prologue, such as the DGProto v1 transport prologue `DGPv1`, is used at a configured B1-R2 service | Handshake does not establish B1-R2; no alternate-prologue fallback. |

## 12.3. Deployment manifest

Before conformance is claimed, the deployment manifest MUST record:

- exact specification, fixture and source hashes, and the compatibility ID;
- ContextID and its authority, and the B1 service tuple and configuration;
- owner-epoch, credential and contact provisioning and revocation authority;
- cryptographic provider APIs, versions, licences, advisory review and erasure limits;
- database, OS, filesystem, keystore, fencing, fsync and crash model;
- time APIs, uncertainty and rebase behaviour;
- all logical, physical and global byte, count, worker and headroom budgets;
- retry, poll, expiry and poison scheduling;
- effect and idempotency capability;
- claimed workload and offline horizon, and measured latency and throughput;
- independent reviewers and unresolved findings.

Protocol constants are fixed in this edition. OS, database and provider selections and measurements are implementation evidence, not missing wire fields or per-platform protocol variants. The manifest MUST distinguish observed results from untested assumptions; unperformed tests, unmeasured benchmarks and unverified licence acceptance MUST NOT be recorded as completed. Independent provider/composition review remains a release gate even though these semantic decisions are fixed.

## 12.4. Assurance status and evidence publication

This edition does not establish implementation conformance or production assurance. Its publication certifies no endpoint, relay or storage implementation, production XEd25519 provider, power-loss campaign, independent interoperability result, benchmark or independent security audit. Gates C01–C14 remain unfulfilled as complete implementation gates. Absence of a report is not a successful result.

Supplemental models, synthetic fixtures or local regression reports, if published, are informative and may be distributed separately. A report must name its exact subject revision or artifact, method, provenance, assumptions and limits; it cannot amend the contract or certify untested behaviour. Reading this specification requires no development archive or unpublished source file.

# 13. Decisions, evidence and references

## 13.1. Decisions made in this edition

| Topic | Selected contract / explicit cost |
|---|---|
| Unified scope | One single-device verified 1:1 protocol, not separate desktop/server editions. |
| Session manager | One active bidirectional DR, four total slots, monotone tuple generations; old receive never reactivates. |
| Renewal | Fresh X3DH+mandatory OPK under same verified IK; no two-phase COMMIT and no promise of lossless old-message recovery. |
| Anti-replay history | Durable high water/masks, 4,096 semantic window and numeric prekey IDs; finite history with explicit stale loss. |
| State/time | Crash-consistent noncloned baseline with trusted OS lease time, explicit time-only cleanup/rebase; no hardware-anchor mandate. |
| Effects/receipts | Immutable encryption; durable inbox/effect distinction; cross-generation receipt targets; UNKNOWN external outcomes preserved. |
| Relay journal | 64-slot sequence window, permanent ACK_FLOOR, reserved control and atomic stale-worker fence; bounded retention of saved observations. |
| Mailbox progress | Snapshot rounds and bounded selection fairness, not FIFO or a latency SLA. |
| Crypto/PQ | Exact classical X3DH/XEd25519/DR CBC-HMAC composition; independent provider/composition review required (Section 12). |
| Binding/evolution | B1-R2 dedicated local tuple, complete-field TLV alignment and padded AAD; profile 2 separates all incompatible proposals. |

Section 12 defines the evidence and distinguishing traces required for these decisions.

## 13.2. Change control and further review

Unpublished working drafts, implementation repositories and private review material are not normative authorities.

Further independent review should address:

- Cryptographic provider acceptance and associated-data composition.
- Lifecycle, replay and late-control counterexamples, and user-visible handling of bounded loss.
- RPC, storage, time and admission consistency.
- B1-R2 control grammar, pin validation and rekey behavior.

Section 12 defines the required implementation evidence. Contradictions and underspecified interoperable behavior require explicit findings and controlled correction under Section 11.4. An editorial revision cannot silently change accepted bytes or state transitions.

## 13.3. External specifications and informative reading

Only the specific algorithms incorporated in Sections 3 and 10 are normative; citing a reference does not import its unrelated options, extensions or session policies. This edition's own rules state its encodings, limits and stricter application checks.

**Incorporated (normative)**

- [1] S. Bradner, *Key words for use in RFCs to Indicate Requirement Levels*, RFC 2119, March 1997, https://www.rfc-editor.org/rfc/rfc2119.html. Requirement language in Section 1.1.
- [2] B. Leiba, *Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words*, RFC 8174, May 2017, https://www.rfc-editor.org/rfc/rfc8174.html. Requirement language in Section 1.1.
- [3] A. Langley, M. Hamburg, S. Turner, *Elliptic Curves for Security*, RFC 7748, January 2016, https://www.rfc-editor.org/rfc/rfc7748.html. X25519 primitive; this edition adds stricter canonical-input and nonzero-result checks.
- [4] Signal, *The X3DH Key Agreement Protocol*, Revision 1, 2016-11-04, https://signal.org/docs/specifications/x3dh/. Sections 2–4: four-DH agreement, prekeys, authentication and fresh responder randomization.
- [5] Signal, *The Double Ratchet Algorithm*, Revision 4, 2025-11-04, https://signal.org/docs/specifications/doubleratchet/. Classical Sections 3.2–3.5, 7.2 and 8: state transitions, CBC/HMAC recommendation and limitations. Post-quantum extensions are not imported.
- [6] Signal, *The XEdDSA and VXEdDSA Signature Schemes*, Revision 1, 2016-10-20, https://signal.org/docs/specifications/xeddsa/. Sections 3 and 5: XEd25519 signing and verification; VXEdDSA is not used.
- [7] H. Krawczyk, P. Eronen, *HMAC-based Extract-and-Expand Key Derivation Function (HKDF)*, RFC 5869, May 2010, https://www.rfc-editor.org/rfc/rfc5869.html. Extract and Expand as instantiated in Section 3.
- [8] T. Perrin, *The Noise Protocol Framework*, Revision 34, 2018-07-11, https://noiseprotocol.org/noise.html. The exact XX pattern and primitives instantiated in Section 10.6; Section 10.7 defines this binding's distinct traffic-key rekey.

**Informative**

- [9] Signal, *The Sesame Algorithm: Session Management for Asynchronous Message Encryption*, Revision 2, 2017-04-14, https://signal.org/docs/specifications/sesame/. Background only; DGProto does not import its inactive-session reactivation or unauthenticated reencrypt retry.

## 13.4. Revision history

| Revision | Date | Status | Changes |
|---|---|---|---|
| 1 | 2026‑11‑12 | Draft | First published revision, superseding the unpublished 2.0.x working drafts. Defines DG2-P2 / B1-R2. A READY intent is fulfilled only on its own SID and is suppressed when that SID is demoted (Sections 6.4 and 8.3). The 16 journal slots kept for control operations are a floor, not a cap (Sections 7.3 and 9.4). |
