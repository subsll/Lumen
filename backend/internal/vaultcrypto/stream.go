// Package vaultcrypto implements Lumen's at-rest encryption format.
//
// Each blob gets a fresh random 256-bit data key (DEK). The DEK is wrapped
// with the master key (KEK) using AES-256-GCM and stored in the header. The
// payload is split into 64 KiB chunks, each sealed with AES-256-GCM using a
// nonce derived from the chunk index and a "final chunk" flag. That prevents
// chunks from being reordered, dropped or the stream being truncated.
//
// Layout:
//
//	magic(8) | wrapNonce(12) | wrappedDEK(48) | chunk*
//	chunk = flag(1) | len(4, big endian) | ciphertext(len)
package vaultcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const ChunkSize = 64 * 1024

var magic = []byte("LUMENV1\x00")

var ErrCorrupt = errors.New("vault: blob is corrupt or has been tampered with")

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("vault: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(idx uint64, final bool) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[:8], idx)
	if final {
		n[11] = 1
	}
	return n
}

// Encrypt reads plaintext from r and writes the encrypted blob to w.
func Encrypt(w io.Writer, r io.Reader, kek []byte) error {
	kekAEAD, err := newGCM(kek)
	if err != nil {
		return err
	}
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return err
	}
	wrapNonce := make([]byte, 12)
	if _, err := rand.Read(wrapNonce); err != nil {
		return err
	}
	wrapped := kekAEAD.Seal(nil, wrapNonce, dek, magic)
	for _, b := range [][]byte{magic, wrapNonce, wrapped} {
		if _, err := w.Write(b); err != nil {
			return err
		}
	}

	aead, err := newGCM(dek)
	if err != nil {
		return err
	}
	buf := make([]byte, ChunkSize)
	hdr := make([]byte, 5)
	var ct []byte
	for idx := uint64(0); ; idx++ {
		n, rerr := io.ReadFull(r, buf)
		final := false
		switch {
		case rerr == io.EOF || rerr == io.ErrUnexpectedEOF:
			final = true
		case rerr != nil:
			return rerr
		}
		ct = aead.Seal(ct[:0], chunkNonce(idx, final), buf[:n], nil)
		hdr[0] = 0
		if final {
			hdr[0] = 1
		}
		binary.BigEndian.PutUint32(hdr[1:], uint32(len(ct)))
		if _, err := w.Write(hdr); err != nil {
			return err
		}
		if _, err := w.Write(ct); err != nil {
			return err
		}
		if final {
			return nil
		}
	}
}

// Decrypt reads an encrypted blob from r and writes plaintext to w.
// Each chunk is authenticated before it is written. Truncation is detected
// when the stream ends before the final chunk.
func Decrypt(w io.Writer, r io.Reader, kek []byte) error {
	kekAEAD, err := newGCM(kek)
	if err != nil {
		return err
	}
	head := make([]byte, len(magic)+12+48)
	if _, err := io.ReadFull(r, head); err != nil {
		return ErrCorrupt
	}
	if string(head[:len(magic)]) != string(magic) {
		return ErrCorrupt
	}
	wrapNonce := head[len(magic) : len(magic)+12]
	dek, err := kekAEAD.Open(nil, wrapNonce, head[len(magic)+12:], magic)
	if err != nil {
		return fmt.Errorf("vault: cannot unwrap data key (wrong master key?): %w", ErrCorrupt)
	}
	aead, err := newGCM(dek)
	if err != nil {
		return err
	}
	hdr := make([]byte, 5)
	ct := make([]byte, ChunkSize+aead.Overhead())
	var pt []byte
	for idx := uint64(0); ; idx++ {
		if _, err := io.ReadFull(r, hdr); err != nil {
			return ErrCorrupt
		}
		final := hdr[0] == 1
		n := binary.BigEndian.Uint32(hdr[1:])
		if hdr[0] > 1 || int(n) > len(ct) {
			return ErrCorrupt
		}
		if _, err := io.ReadFull(r, ct[:n]); err != nil {
			return ErrCorrupt
		}
		pt, err = aead.Open(pt[:0], chunkNonce(idx, final), ct[:n], nil)
		if err != nil {
			return ErrCorrupt
		}
		if _, err := w.Write(pt); err != nil {
			return err
		}
		if final {
			var one [1]byte
			if _, err := r.Read(one[:]); err != io.EOF {
				return ErrCorrupt
			}
			return nil
		}
	}
}
