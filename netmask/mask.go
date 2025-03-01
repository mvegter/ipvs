// Portions of netmask adapted from the Go Standard Library.
// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the go.LICENSE file.

// Package netmask defiens a value type representing an
// network mask for IPv4 and IPv6.
//
// Compared to the net.IPMask type, this package takes
// less memory, is immutable, and is comparable.
package netmask

import (
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
)

// Mask represents an IPv4 mask or an IPv6 prefix, similar to net.IPMask or netip.Prefix.
//
// Unlike net.IPMask, Mask is a comparable value type (it supports == and can be map key) and
// is immutable.
//
// Unlike netip.Prefix, Mask is not attached to an IP address, and does not require IPv4 masks
// to be a prefix.
type Mask struct {
	// mask
	mask uint32

	// z is the mask's address family
	//
	// 0 means an invalid Mask (the zero Mask)
	// z4 means an IPv4 address.
	// z6 means an IPv6 address.
	z int8
}

// z0, z4, and z6 are sentinel Mask.z values.
const (
	z0 int8 = iota
	z4
	z6
)

// MaskFrom4 returns the IPv4 mask given by the bytes in mask.
func MaskFrom4(mask [4]byte) Mask {
	return Mask{
		mask: uint32(uint32(mask[0])<<24 | uint32(mask[1])<<16 | uint32(mask[2])<<8 | uint32(mask[3])),
		z:    z4,
	}
}

// MaskFrom16 returns the IPv6 prefix given by the prefix in mask.
// Note that if the prefix is not one bits followed by all zero bits
// the invalid Mask is returned.
func MaskFrom16(mask [16]byte) Mask {
	ones := prefixLength(mask[:])
	if ones == -1 {
		return Mask{}
	}

	return Mask{
		mask: uint32(ones),
		z:    z6,
	}
}

// MaskFromSlice parses the 4- or 16-byte slices as an IPv4 netmask or IPv6 prefix.
// Note that a net.IPMask can by passed directly as the []byte argument. IIf slice's
// length is not 4 or 16, MaskFromSlice returns Mask{}, false.
func MaskFromSlice(mask []byte) (Mask, bool) {
	switch len(mask) {
	case 4:
		return MaskFrom4(*(*[4]byte)(mask)), true
	case 16:
		return MaskFrom16(*(*[16]byte)(mask)), true
	}

	return Mask{}, false
}

// prefixLength returns the number of leading one bits in mask. If the prefix isn't
// followed entirely by zero bits, -1 is returned.
func prefixLength(mask []byte) int {
	var n int
	for i, v := range mask {
		if v == 0xFF {
			n += 8
			continue
		}
		for v&0x80 != 0 {
			n++
			v <<= 1
		}
		if v != 0 {
			return -1
		}
		for i++; i < len(mask); i++ {
			if mask[i] != 0 {
				return -1
			}
		}
		break
	}
	return n
}

// AsSlice returns an IPv4 or IPv6 mask in its respective 4-byte or 16-byte representation.
func (mask Mask) AsSlice() []byte {
	switch mask.z {
	case z0:
		return nil
	case z4:
		var ret [4]byte
		binary.BigEndian.PutUint32(ret[:], mask.mask)
		return ret[:]
	default:
		var ret [16]byte
		n := uint(mask.mask)
		for i := 0; i < 16; i++ {
			if n >= 8 {
				ret[i] = 0xFF
				n -= 8
				continue
			}
			ret[i] = ^byte(0xFF >> n)
			n = 0
		}
		return ret[:]
	}
}

// MaskFrom returns a Mask consisting of 'ones' 1 bits followed by 0s up to a total length
// of 'bits' bits.
func MaskFrom(ones, bits int) Mask {
	if ones < 0 || ones > bits {
		return Mask{}
	}

	switch bits {
	case 32:
		var mask [4]byte
		n := uint(ones)
		for i := 0; i < 4; i++ {
			if n >= 8 {
				mask[i] = 0xFF
				n -= 8
				continue
			}
			mask[i] = ^byte(0xFF >> n)
			n = 0
		}
		return MaskFrom4(mask)
	case 128:
		return Mask{
			mask: uint32(ones),
			z:    z6,
		}
	default:
		return Mask{}
	}
}

// IsValid reports whether the Mask is an initialized mask (not the zero Mask).
//
// Note that a non-prefix mask is considered valid, even for IPv6.
func (mask Mask) IsValid() bool {
	return mask.z != z0
}

// Is4 reports whether the Mask is for IPv4.
func (mask Mask) Is4() bool {
	return mask.z == z4
}

// Is6 reports whether the mask is for IPv6.
func (mask Mask) Is6() bool {
	return mask.z == z6
}

// Bits returns the masks's prefix length.
//
// It reports -1 if the mask does not contain a prefix.
func (mask Mask) Bits() int {
	switch mask.z {
	case z0:
		return -1
	case z4:
		mask := mask.AsSlice()
		return prefixLength(mask)
	default:
		return int(mask.mask)
	}
}

// AppendBinary implements the [encoding.BinaryAppender] interface.
func (mask Mask) AppendBinary(b []byte) ([]byte, error) {
	switch mask.z {
	case z0:
		return b, nil
	case z4:
		return append(b,
			byte(mask.mask>>24),
			byte(mask.mask>>16&0xFF),
			byte(mask.mask>>8&0xFF),
			byte(mask.mask&0xFF),
		), nil
	default:
		return append(b, byte(mask.mask)), nil
	}
}

// MarshalBinary implements the [encoding.BinaryMarshaler] interface.
// It returns a zero-length slice for the zero Mask, the 4-byte mask
// for IPv4, and a 1-byte prefix for IPv6.
func (mask Mask) MarshalBinary() ([]byte, error) {
	return mask.AppendBinary(make([]byte, 0, mask.marshalBinarySize()))
}

func (mask Mask) marshalBinarySize() int {
	switch mask.z {
	case z0:
		return 0
	case z4:
		return 4
	default:
		return 1
	}
}

// UnmarshalBinary implements the [encoding.BinaryUnmarshaler] interface. It
// expects data in the form generated by MarshalBinary.
func (mask *Mask) UnmarshalBinary(b []byte) error {
	n := len(b)
	switch {
	case n == 0:
		*mask = Mask{}
		return nil
	case n == 4:
		*mask = MaskFrom4(*(*[4]byte)(b))
		return nil
	case n == 1:
		*mask = MaskFrom(int(b[0]), 128)
		return nil
	}

	return errors.New("unexpected slice size")
}

// AppendText implements the [encoding.TextAppender] interface.
func (mask Mask) AppendText(b []byte) ([]byte, error) {
	switch mask.z {
	case z0:
		return b, nil
	case z4:
		return appendTextIPv4(mask, b), nil
	default:
		return strconv.AppendUint(b, uint64(mask.mask), 10), nil
	}
}

// MarshalText implements the [encoding.TextMarshaler] interface. The encoding is
// the same as returned by String, with one exception: If mask is the zero Mask,
// the encoding is the empty string.
func (mask Mask) MarshalText() ([]byte, error) {
	return mask.AppendText(make([]byte, 0, mask.marshalTextSize()))
}

func (mask Mask) marshalTextSize() int {
	switch mask.z {
	case z0:
		return 0
	case z4:
		return len("255.255.255.255")
	default:
		return 1
	}
}

// UnmarshalText implements the [encoding.TextUnmarshaler] interface. The mask
// is expected in a form generated by MarshalText.
func (mask *Mask) UnmarshalText(text []byte) error {
	n := len(text)
	switch {
	case n == 0:
		*mask = Mask{}
		return nil
	case n >= 1 && n <= 3:
		u, err := strconv.ParseUint(string(text[:]), 10, 64)
		if err != nil {
			return err
		}
		*mask = MaskFrom(int(u), 128)
		return nil
	case n >= len("1.1.1.1") && n <= len("255.255.255.255"):
		sub := strings.SplitN(string(text), ".", 4)
		if len(sub) != 4 {
			return errors.New("unexpected slice type")
		}

		var fields [4]uint32
		for i, s := range sub {
			f, err := strconv.ParseUint(s, 10, 8)
			if err != nil {
				return err
			}
			fields[i] = uint32(f)
		}
		*mask = Mask{mask: uint32(fields[0]<<24 | fields[1]<<16 | fields[2]<<8 | fields[3]), z: z4}
		return nil
	}

	return errors.New("unexpected slice size")
}

// String returns the string form of the Mask mask. It returns one of these forms:
//
// - "invalid Mask", if mask is the zero Mask
// - IPv4 dotted decimal ("255.255.255.0")
// - IPv6 prefix ("64")
func (mask Mask) String() string {
	switch mask.z {
	case z0:
		return "invalid Mask"
	case z4:
		b := make([]byte, 0, len("255.255.255.255"))
		return string(appendTextIPv4(mask, b))
	default:
		return strconv.FormatUint(uint64(mask.mask), 10)
	}
}

func (x Mask) Equal(y Mask) bool {
	return x == y
}

func appendTextIPv4(mask Mask, b []byte) []byte {
	b = strconv.AppendUint(b, uint64(uint8(mask.mask>>24)), 10)
	b = append(b, '.')
	b = strconv.AppendUint(b, uint64(uint8(mask.mask>>16)), 10)
	b = append(b, '.')
	b = strconv.AppendUint(b, uint64(uint8(mask.mask>>8)), 10)
	b = append(b, '.')
	b = strconv.AppendUint(b, uint64(uint8(mask.mask)), 10)
	return b
}
