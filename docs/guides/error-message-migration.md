# ErrorMessage wire migration

Error (`0x09`) now follows the normative `code:u16 LE || context:raw UTF-8`
layout. There is no text TLV, alignment padding, length prefix, negotiation,
or heuristic fallback. SessionClose (`0x05`) still uses its optional type-1 TLV.
The 40-byte header, version, encryption, handshake, session and rekey semantics
are unchanged.

For code 9 and `oops`, the old payload was `09000104006f6f707300`; the new
payload is **`09006f6f7073`**. For code 3 and `hi`, old
`03000102006869000000` becomes **`03006869`**. Empty context remains two code
bytes; that case alone cannot distinguish generations. Old TLV bytes may also
be valid raw UTF-8 (including control characters and NUL), so they cannot always
be rejected. A successful handshake or decode cannot identify an unknown peer's
codec generation. Do not attempt mixed-generation detection from payload text.

## Coordinated deployment and rollback

1. Inventory every sender and receiver of Error payloads, including custom
   codecs, persisted payloads and diagnostic tooling. Establish exact revisions
   out of band; do not assume compatibility from protocol version 1.
2. Rebuild both Go and Rust peers together. Test nonempty Error text and Close
   against the matched revisions before deploying.
3. Drain old connections and stop new mixed-generation admission using your
   deployment controls. Deploy the paired binaries, then reconnect clients.
   The library does not add automatic reconnect or a wire-generation handshake.
4. If rolling back, drain again and roll back **both** peer generations as a
   pair before reconnecting. Keep persisted old payloads separately labeled.

## API and limits

Go keeps `Code uint16` and `Context string`; `MaxErrorContextSize` is an additive
constant. Rust keeps `code: u8` and `reason: String` for source compatibility.
Its private raw helper handles u16, but the public decoder returns existing
`InvalidErrorCode` for codes above 255 **after** raw length/UTF-8 validation.
This is not full-u16 Go/Rust interoperability; paired public use is codes 0–255.
No variant is added to Rust's exhaustive `ApplicationMessage` API.

Raw context is at most **65477 bytes**, `65535 - 40 - 16 - 2`, with zero frame
padding. Subtract actual frame padding when sending. Count UTF-8 bytes, not
characters. Empty text and embedded NUL are valid. Close retains its distinct
65473-byte aligned-TLV text limit (`MaxReasonSize` in Go).

Go marshal preserves `ErrInvalidUTF8` before `ErrReasonTooLong`; raw decode
returns `ErrMessageTooShort` for fewer than two bytes, `ErrMessageLength` for
more than 65479 bytes before copying/UTF-8 validation, and `ErrInvalidUTF8` for
invalid context. Raw Error never returns TLV validation errors. Successful
buffers/text are owned; decode errors leave an existing destination unchanged.

## Validation scope

Shared hand-authored Error vectors cover code 9/`oops`, 255/`Ошибка`, 3/`hi`,
NUL, short input and invalid UTF-8. The retained empty vector is not migration
proof. Go code `0x1234`/`hi` is `34126869`; Rust tests distinguish private raw
acceptance from public narrowing rejection. Vectors are literal codec oracles,
not evidence of a live cross-language encrypted connection or production rollout.
