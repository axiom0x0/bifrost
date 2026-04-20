// bifrost-crypto.js — Pure JS AES-256-GCM (fallback when Web Crypto unavailable)
// Implements the same nonce-prepended ciphertext format as the Go server.
// Uses Web Crypto API when available, falls back to pure JS otherwise.

var BifrostCrypto = (function() {
  'use strict';

  // Try Web Crypto only on secure contexts (HTTPS/localhost)
  var isSecure = (typeof window !== 'undefined' && window.isSecureContext) ||
                 (typeof self !== 'undefined' && self.isSecureContext) ||
                 (typeof window === 'undefined'); // Node.js
  var subtle = isSecure && (typeof crypto !== 'undefined' && crypto.subtle) ? crypto.subtle : null;

  // --- Pure JS AES-256-GCM implementation ---
  // Based on the AES block cipher + GCM mode of operation.

  // AES S-box
  var SBOX = new Uint8Array([
    0x63,0x7c,0x77,0x7b,0xf2,0x6b,0x6f,0xc5,0x30,0x01,0x67,0x2b,0xfe,0xd7,0xab,0x76,
    0xca,0x82,0xc9,0x7d,0xfa,0x59,0x47,0xf0,0xad,0xd4,0xa2,0xaf,0x9c,0xa4,0x72,0xc0,
    0xb7,0xfd,0x93,0x26,0x36,0x3f,0xf7,0xcc,0x34,0xa5,0xe5,0xf1,0x71,0xd8,0x31,0x15,
    0x04,0xc7,0x23,0xc3,0x18,0x96,0x05,0x9a,0x07,0x12,0x80,0xe2,0xeb,0x27,0xb2,0x75,
    0x09,0x83,0x2c,0x1a,0x1b,0x6e,0x5a,0xa0,0x52,0x3b,0xd6,0xb3,0x29,0xe3,0x2f,0x84,
    0x53,0xd1,0x00,0xed,0x20,0xfc,0xb1,0x5b,0x6a,0xcb,0xbe,0x39,0x4a,0x4c,0x58,0xcf,
    0xd0,0xef,0xaa,0xfb,0x43,0x4d,0x33,0x85,0x45,0xf9,0x02,0x7f,0x50,0x3c,0x9f,0xa8,
    0x51,0xa3,0x40,0x8f,0x92,0x9d,0x38,0xf5,0xbc,0xb6,0xda,0x21,0x10,0xff,0xf3,0xd2,
    0xcd,0x0c,0x13,0xec,0x5f,0x97,0x44,0x17,0xc4,0xa7,0x7e,0x3d,0x64,0x5d,0x19,0x73,
    0x60,0x81,0x4f,0xdc,0x22,0x2a,0x90,0x88,0x46,0xee,0xb8,0x14,0xde,0x5e,0x0b,0xdb,
    0xe0,0x32,0x3a,0x0a,0x49,0x06,0x24,0x5c,0xc2,0xd3,0xac,0x62,0x91,0x95,0xe4,0x79,
    0xe7,0xc8,0x37,0x6d,0x8d,0xd5,0x4e,0xa9,0x6c,0x56,0xf4,0xea,0x65,0x7a,0xae,0x08,
    0xba,0x78,0x25,0x2e,0x1c,0xa6,0xb4,0xc6,0xe8,0xdd,0x74,0x1f,0x4b,0xbd,0x8b,0x8a,
    0x70,0x3e,0xb5,0x66,0x48,0x03,0xf6,0x0e,0x61,0x35,0x57,0xb9,0x86,0xc1,0x1d,0x9e,
    0xe1,0xf8,0x98,0x11,0x69,0xd9,0x8e,0x94,0x9b,0x1e,0x87,0xe9,0xce,0x55,0x28,0xdf,
    0x8c,0xa1,0x89,0x0d,0xbf,0xe6,0x42,0x68,0x41,0x99,0x2d,0x0f,0xb0,0x54,0xbb,0x16
  ]);

  // Inverse S-box
  var SBOX_INV = new Uint8Array(256);
  for (var si = 0; si < 256; si++) SBOX_INV[SBOX[si]] = si;

  // Rcon
  var RCON = [0x01,0x02,0x04,0x08,0x10,0x20,0x40,0x80,0x1b,0x36];

  // GF(2^8) multiply
  function gmul(a, b) {
    var p = 0;
    for (var i = 0; i < 8; i++) {
      if (b & 1) p ^= a;
      var hi = a & 0x80;
      a = (a << 1) & 0xff;
      if (hi) a ^= 0x1b;
      b >>= 1;
    }
    return p;
  }

  // Key expansion for AES-256 (14 rounds, 60 32-bit words)
  function expandKey(key) {
    var Nk = 8, Nr = 14, Nb = 4;
    var W = new Uint32Array(Nb * (Nr + 1));
    for (var i = 0; i < Nk; i++) {
      W[i] = (key[4*i]<<24) | (key[4*i+1]<<16) | (key[4*i+2]<<8) | key[4*i+3];
    }
    for (var i = Nk; i < Nb*(Nr+1); i++) {
      var t = W[i-1];
      if (i % Nk === 0) {
        t = ((t << 8) | (t >>> 24)) >>> 0;
        t = (SBOX[(t>>>24)&0xff]<<24) | (SBOX[(t>>>16)&0xff]<<16) | (SBOX[(t>>>8)&0xff]<<8) | SBOX[t&0xff];
        t = (t ^ (RCON[(i/Nk)-1] << 24)) >>> 0;
      } else if (i % Nk === 4) {
        t = (SBOX[(t>>>24)&0xff]<<24) | (SBOX[(t>>>16)&0xff]<<16) | (SBOX[(t>>>8)&0xff]<<8) | SBOX[t&0xff];
      }
      W[i] = (W[i-Nk] ^ t) >>> 0;
    }
    return W;
  }

  // AES block encrypt (16 bytes)
  function aesEncryptBlock(block, W) {
    var s = new Uint8Array(16);
    for (var i = 0; i < 16; i++) s[i] = block[i];

    // AddRoundKey
    for (var i = 0; i < 4; i++) {
      var w = W[i];
      s[4*i] ^= (w>>>24)&0xff; s[4*i+1] ^= (w>>>16)&0xff;
      s[4*i+2] ^= (w>>>8)&0xff; s[4*i+3] ^= w&0xff;
    }

    for (var round = 1; round <= 14; round++) {
      // SubBytes
      for (var i = 0; i < 16; i++) s[i] = SBOX[s[i]];

      // ShiftRows
      var t;
      t = s[1]; s[1] = s[5]; s[5] = s[9]; s[9] = s[13]; s[13] = t;
      t = s[2]; s[2] = s[10]; s[10] = t; t = s[6]; s[6] = s[14]; s[14] = t;
      t = s[15]; s[15] = s[11]; s[11] = s[7]; s[7] = s[3]; s[3] = t;

      // MixColumns (skip on last round)
      if (round < 14) {
        for (var c = 0; c < 4; c++) {
          var i = c*4;
          var a0=s[i], a1=s[i+1], a2=s[i+2], a3=s[i+3];
          s[i]   = gmul(2,a0) ^ gmul(3,a1) ^ a2 ^ a3;
          s[i+1] = a0 ^ gmul(2,a1) ^ gmul(3,a2) ^ a3;
          s[i+2] = a0 ^ a1 ^ gmul(2,a2) ^ gmul(3,a3);
          s[i+3] = gmul(3,a0) ^ a1 ^ a2 ^ gmul(2,a3);
        }
      }

      // AddRoundKey
      for (var i = 0; i < 4; i++) {
        var w = W[round*4+i];
        s[4*i] ^= (w>>>24)&0xff; s[4*i+1] ^= (w>>>16)&0xff;
        s[4*i+2] ^= (w>>>8)&0xff; s[4*i+3] ^= w&0xff;
      }
    }
    return s;
  }

  // GCM: multiply in GF(2^128)
  function ghashBlock(H, X, Y) {
    // Y = (Y ^ X) * H in GF(2^128)
    var v = new Uint8Array(16);
    for (var i = 0; i < 16; i++) v[i] = Y[i] ^ X[i];

    var z = new Uint8Array(16);
    for (var i = 0; i < 16; i++) {
      for (var j = 7; j >= 0; j--) {
        if ((v[i] >> j) & 1) {
          for (var k = 0; k < 16; k++) z[k] ^= H[k];
        }
        // multiply H by x in GF(2^128) with reduction polynomial
        var carry = H[15] & 1;
        for (var k = 15; k > 0; k--) {
          H[k] = ((H[k] >>> 1) | ((H[k-1] & 1) << 7)) & 0xff;
        }
        H[0] = (H[0] >>> 1) & 0xff;
        if (carry) H[0] ^= 0xe1;
      }
    }
    return z;
  }

  // GCM GHASH
  function ghash(Hblock, aad, ciphertext) {
    var Y = new Uint8Array(16);
    var H;

    // Process AAD (empty for our use case)
    var aadLen = aad ? aad.length : 0;
    for (var i = 0; i < aadLen; i += 16) {
      var block = new Uint8Array(16);
      var end = Math.min(i + 16, aadLen);
      for (var j = i; j < end; j++) block[j - i] = aad[j];
      H = new Uint8Array(Hblock);
      Y = ghashBlock(H, block, Y);
    }

    // Process ciphertext
    for (var i = 0; i < ciphertext.length; i += 16) {
      var block = new Uint8Array(16);
      var end = Math.min(i + 16, ciphertext.length);
      for (var j = i; j < end; j++) block[j - i] = ciphertext[j];
      H = new Uint8Array(Hblock);
      Y = ghashBlock(H, block, Y);
    }

    // Length block: aad_bits (64) || ct_bits (64)
    var lenBlock = new Uint8Array(16);
    var aadBits = aadLen * 8;
    var ctBits = ciphertext.length * 8;
    lenBlock[4] = (aadBits >>> 24) & 0xff;
    lenBlock[5] = (aadBits >>> 16) & 0xff;
    lenBlock[6] = (aadBits >>> 8) & 0xff;
    lenBlock[7] = aadBits & 0xff;
    lenBlock[12] = (ctBits >>> 24) & 0xff;
    lenBlock[13] = (ctBits >>> 16) & 0xff;
    lenBlock[14] = (ctBits >>> 8) & 0xff;
    lenBlock[15] = ctBits & 0xff;
    H = new Uint8Array(Hblock);
    Y = ghashBlock(H, lenBlock, Y);

    return Y;
  }

  // Increment counter (last 4 bytes, big-endian)
  function incCounter(counter) {
    var c = new Uint8Array(counter);
    for (var i = 15; i >= 12; i--) {
      c[i]++;
      if (c[i] !== 0) break;
    }
    return c;
  }

  // AES-GCM encrypt
  function aesGcmEncrypt(key, nonce, plaintext) {
    var W = expandKey(key);

    // H = AES(K, 0^128)
    var Hblock = aesEncryptBlock(new Uint8Array(16), W);

    // Initial counter: nonce || 0x00000001
    var J0 = new Uint8Array(16);
    for (var i = 0; i < 12; i++) J0[i] = nonce[i];
    J0[15] = 1;

    // Encrypt plaintext with CTR mode starting from J0+1
    var counter = incCounter(J0);
    var ciphertext = new Uint8Array(plaintext.length);
    for (var i = 0; i < plaintext.length; i += 16) {
      var keystream = aesEncryptBlock(counter, W);
      var end = Math.min(i + 16, plaintext.length);
      for (var j = i; j < end; j++) {
        ciphertext[j] = plaintext[j] ^ keystream[j - i];
      }
      counter = incCounter(counter);
    }

    // Compute auth tag: GHASH ^ AES(K, J0)
    var S = ghash(Hblock, null, ciphertext);
    var tagMask = aesEncryptBlock(J0, W);
    var tag = new Uint8Array(16);
    for (var i = 0; i < 16; i++) tag[i] = S[i] ^ tagMask[i];

    // Return ciphertext || tag
    var result = new Uint8Array(ciphertext.length + 16);
    result.set(ciphertext);
    result.set(tag, ciphertext.length);
    return result;
  }

  // AES-GCM decrypt
  function aesGcmDecrypt(key, nonce, data) {
    if (data.length < 16) throw new Error('Data too short');

    var ciphertext = data.slice(0, data.length - 16);
    var tag = data.slice(data.length - 16);

    var W = expandKey(key);

    // H = AES(K, 0^128)
    var Hblock = aesEncryptBlock(new Uint8Array(16), W);

    // Initial counter
    var J0 = new Uint8Array(16);
    for (var i = 0; i < 12; i++) J0[i] = nonce[i];
    J0[15] = 1;

    // Verify tag first
    var S = ghash(Hblock, null, ciphertext);
    var tagMask = aesEncryptBlock(J0, W);
    for (var i = 0; i < 16; i++) {
      if (tag[i] !== ((S[i] ^ tagMask[i]) & 0xff)) {
        throw new Error('Authentication failed');
      }
    }

    // Decrypt with CTR mode
    var counter = incCounter(J0);
    var plaintext = new Uint8Array(ciphertext.length);
    for (var i = 0; i < ciphertext.length; i += 16) {
      var keystream = aesEncryptBlock(counter, W);
      var end = Math.min(i + 16, ciphertext.length);
      for (var j = i; j < end; j++) {
        plaintext[j] = ciphertext[j] ^ keystream[j - i];
      }
      counter = incCounter(counter);
    }

    return plaintext;
  }

  // --- Public API ---

  // Parse base64url key from URL fragment
  function parseKeyFromFragment() {
    var frag = window.location.hash.substring(1);
    if (!frag) return null;
    var b64 = frag.replace(/-/g,'+').replace(/_/g,'/');
    while (b64.length % 4) b64 += '=';
    var raw = atob(b64);
    var bytes = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
    return bytes;
  }

  // Encrypt: returns Uint8Array(nonce + ciphertext + tag)
  // Format matches Go: nonce(12) || ciphertext || tag(16)
  async function encrypt(keyBytes, plaintext) {
    if (subtle) {
      try {
        var key = await subtle.importKey('raw', keyBytes, 'AES-GCM', false, ['encrypt']);
        var iv = new Uint8Array(12);
        crypto.getRandomValues(iv);
        var encrypted = await subtle.encrypt({ name: 'AES-GCM', iv: iv }, key, plaintext);
        var result = new Uint8Array(12 + encrypted.byteLength);
        result.set(iv);
        result.set(new Uint8Array(encrypted), 12);
        return result;
      } catch(e) { /* fall through to pure JS */ }
    }
    var iv = new Uint8Array(12);
    if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
      crypto.getRandomValues(iv);
    } else {
      for (var i = 0; i < 12; i++) iv[i] = Math.floor(Math.random() * 256);
    }
    var pt = new Uint8Array(plaintext instanceof ArrayBuffer ? plaintext : plaintext.buffer || plaintext);
    var ct = aesGcmEncrypt(keyBytes, iv, pt);
    var result = new Uint8Array(12 + ct.length);
    result.set(iv);
    result.set(ct, 12);
    return result;
  }

  // Decrypt: expects Uint8Array(nonce + ciphertext + tag)
  async function decrypt(keyBytes, data) {
    var arr = new Uint8Array(data instanceof ArrayBuffer ? data : data.buffer || data);
    var iv = arr.slice(0, 12);
    var payload = arr.slice(12); // ciphertext + tag

    if (subtle) {
      try {
        var key = await subtle.importKey('raw', keyBytes, 'AES-GCM', false, ['decrypt']);
        var plain = await subtle.decrypt({ name: 'AES-GCM', iv: iv }, key, payload);
        return new Uint8Array(plain);
      } catch(e) { /* Web Crypto failed, fall through to pure JS */ }
    }
    return aesGcmDecrypt(keyBytes, iv, payload);
  }

  return {
    parseKeyFromFragment: parseKeyFromFragment,
    encrypt: encrypt,
    decrypt: decrypt,
    hasWebCrypto: !!subtle
  };
})();
