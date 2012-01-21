/*
 * Package deltacolomb implements order-zero exponential Golomb
 * coding, and provides wrapper functions that take an array
 * of integers, delta-encode them, and then encode the residuals
 * using Exp-Golomb.
 *
 * The core Exp-Golomb functions mirror those of pkg/compress:
 *
 * encoder := NewExpGolombEncoder(w)
 * encoder.Write([]int{0, 0, 1, 1})
 * // The encoder will call w.Write() as necessary.
 *
 * decoder := NewExpGolombDecoder(r)
 * decoder.Read(buf)
 * // the decoder will call r.Read() as necessary.
 *
 * At present, this code is not optimized for speed.
 */

package deltagolomb

import (
	"io"
	"bytes"
	"bufio"
)

type ExpGolombDecoder struct {
	r byteReader
	b byte
	state int
	val int
	zeros int
	nBits int
}

type ExpGolombEncoder struct {
	data   byte
	bitpos uint
	out byteWriter
}	

// Create a new Exp-Golomb stream Encoder.
// Accepts integers via the Write( []int ) method, and writes
// the resulting byte stream to w.  Users must call Close()
// when finished to ensure that all bytes are written to w.
func NewExpGolombEncoder(w io.Writer) *ExpGolombEncoder {
	ww := makeWriter(w)
	return &ExpGolombEncoder{0, 0, ww}
}

// Create a new Exp-Golomb stream decoder.  Callers can read
// decoded integers via the Read( []int ) method.  Reads bytes
// from r as needed and as they become available.
func NewExpGolombDecoder(r io.Reader) *ExpGolombDecoder{ 
	d := &ExpGolombDecoder{}
	d.r = makeReader(r)
	return d
}

// Helper function stolen from compress/flate/inflate.go
// If the passed in reader does not support ReadByte(), wrap
// it in a bufio.
type byteReader interface {
	io.Reader
	ReadByte() (c byte, err error)
}

// Analogous helper for byte-at-a-time output.
// If the passed in writer does not support WriteByte(),
// wrap it in a bufio.
type byteWriter interface {
	io.Writer
	WriteByte(c byte) error
	Flush() error
}

func makeReader(r io.Reader) byteReader {
	if rr, ok := r.(byteReader); ok {
		return rr
	}
	return bufio.NewReader(r)
}

func makeWriter(w io.Writer) byteWriter {
	if ww, ok := w.(byteWriter); ok {
		return ww
	}
	return bufio.NewWriter(w)
}

// Decode states, bit-at-a-time (slow but safe)
const (
	COUNTING_ZEROS = iota
	SHIFTING_BITS
	READING_SIGN
)

// Encode a slice of signed integers into a byte stream.
// Output bytes are buffered and may not be entirely written
// until the encoder is Close()'d.

func (s *ExpGolombEncoder) Write(ilist []int) {
	for _, i := range ilist {
		s.add(i)
	}
}

// Encode a single signed integer into the byte stream.
func (s *ExpGolombEncoder) WriteInt(i int) {
	s.add(i)
}

func (s *ExpGolombEncoder) Close() {
	if (s.bitpos != 0) {
		s.out.WriteByte(s.data)
	}
	s.data = 0
	s.bitpos = 0
	s.out.Flush()
}

// Decode a byte-stream of exp-golomb coded signed integers.
// Reads all available bytes from 'in';
// Emits decoded integers to 'out'.
func (s *ExpGolombDecoder) Read(out []int) (int, error) {
	cpos := 0
	n := len(out)

	for {
		if (s.nBits == 0) {
			var readError error
			s.b, readError = s.r.ReadByte()
			if readError != nil {
				return cpos, readError
			} else {
				s.nBits = 8
			}
		}
		for s.nBits > 0 {
			if cpos >= n {
				return cpos, nil
			}
			bit := (s.b >> (uint(s.nBits - 1))) & 0x01
			s.nBits--

			switch s.state {
			case COUNTING_ZEROS:
				if bit == 0 {
					s.zeros++
				} else {
					if s.zeros == 0 {
						out[cpos] = 0
						cpos++
					} else {
						s.state = SHIFTING_BITS
						s.val = 1
					}
				}
			case SHIFTING_BITS:
				s.val <<= 1
				s.val |= int(bit)
				s.zeros--
				if s.zeros == 0 {
					s.val -= 1 // Because we stole bit for 0.
					s.state = READING_SIGN
				}
			case READING_SIGN:
				if bit == 1 {
					s.val = -s.val
				}
				out[cpos] = s.val
				cpos++
				s.state = COUNTING_ZEROS
			}
		}
	}
	// If we run off the end, do not emit the value.
	return 0, nil // NOTREACHED
}

// Exponential golomb coding with an explicit sign bit for everything
// except zero.
// 0 = 1
// 1 = 010{sign}    sign:  0 = positive, 1 = negative.
// 2 = 011{sign}
// 3 = 00100{sign}
// 4 = 00101{sign}
// 5 = 00110{sign}
// 6 = 00111{sign}
// ...
// If we don't fill the byte, just leave it as zeros.  The decode
// will run off the end in counting zeros and emit nothing.

// Add implements the actual encoding of a single value.  Emits
// zero or more bytes onto the 'out' stream as they are filled.
// Note:  This function is only safe for values up to += 2^31 - 2,
// not 2^31 - 1 as you might expect.  Rewrite to take uint64s if
// needed for larger values.

func (s *ExpGolombEncoder) add(item int) {
	// Quick optimization for the most common values we expect to encode.
	// This has an obvious generalization to a small table if desired.
	switch item {
	case 0:
		s.addBits(1, 1)
		return
	case 1:
		s.addBits(0x4, 4)
		return
	case -1:
		s.addBits(0x5, 4)
		return
	}

	sign := uint(0)
	if item < 0 {
		sign = 1
		item = -item
	}

	uitem := uint(item)
	uitem += 1 // we stole a bit for zero.
	nbits := uint(bitLen(uitem) - 1)
	s.addZeroBits(nbits)
	uitem = (uitem << 1) | sign
	s.addBits(uitem, nbits+2) // +1 high order, +1 sign
	return
}

// Helper function that adds nbits bit to the output byte stream.
// Emits the byte(s) if they are full, otherwise just updates internal
// state.
func (s *ExpGolombEncoder) addBits(bits uint, nbits uint) {
	bitsleft := uint(8) - s.bitpos
	if nbits < bitsleft {
		s.data |= (byte(bits) << (bitsleft - nbits))
		s.bitpos += nbits
		return
	} else {
		s.data |= byte(bits >> (nbits - bitsleft))
		s.out.WriteByte(s.data)
		s.bitpos = 0
		s.data = 0
		nbits -= bitsleft
	}

	for ; nbits > 8; nbits -= 8 {
		s.data = byte((bits >> (nbits - 8)) & 0xff)
		s.out.WriteByte(s.data)
	}
	s.data = byte((bits << (8 - nbits))  & 0xff)
	s.bitpos = nbits
}

// Helper function specialized to add zeros to the output stream
func (s *ExpGolombEncoder) addZeroBits(nzeros uint) {
	// Split into three chunks:  Number of zeros we can add
	// to the current byte;  number of intermediate zero bytes
	// we should emit;  number of zeros to add to the new byte
	// if any.
	if nzeros < (8 - s.bitpos) {
		s.bitpos += nzeros
		return
	} else {
		nzeros -= (8 - s.bitpos)
		s.out.WriteByte(s.data)
		s.data = 0
		s.bitpos = 0
	}
	// We now have a zero byte at bitpos 0.
	for ; nzeros >= 8; nzeros -= 8 {
		s.out.WriteByte(s.data)
	}
	s.bitpos += nzeros
}

// Computes the number of bits needed to represent a value.
// Stolen from arith.go;  it's not exported there.
func bitLen(x uint) (n int) {
	for ; x >= 0x100; x >>= 8 {
		n += 8
	}
	for ; x > 0; x >>= 1 {
		n++
	}
	return
}

// Delta encodes an array of integers and then uses Exp-Golomb to
// encode the residuals.  Returns the encoded byte stream of residuals
// as a byte array.
// DeltaEncode uses the value of 'start' to encode the first value
// as value - start.
func DeltaEncode(start int, data []int) []byte {
	bytestream := &bytes.Buffer{}
	egs := NewExpGolombEncoder(bytestream)

	prev := start
	for _, i := range data {
		delta := i - prev
		prev = i
		egs.Write([]int{delta})
	}
	egs.Close()

	return bytestream.Bytes()
}

// Decodes an array of bytes representing an Exp-Golomb encoded
// stream of residuals of delta compression.  Returns the
// results as an array of integers.
func DeltaDecode(base int, compressed []byte) []int {
	res := make([]int, 0)
	val := base
	decoder := NewExpGolombDecoder(bytes.NewBuffer(compressed))

	tmp := make([]int, 1)
	for {
		n, err := decoder.Read(tmp)
		if (n > 0) {
			val = val+tmp[0]
			res = append(res, val)
		}
		if err != nil {
			return res
		}
	}
	return res // NOTREACHED - compiler doesn't know it.
}
