/*
 * Package deltacolomb implements order-zero exponential Golomb
 * coding, and provides wrapper functions that take an array
 * of integers, delta-encode them, and then encode the residuals
 * using Exp-Golomb.
 *
 * The core Exp-Golomb functions offer a channel interface that
 * emit/accept bytes on one channel and integers on the other
 * channel for encoding and decoding, respectively.  Example
 * use:
 *
 * ints := make(chan int)
 * bytes := make(chan byte)
 * eg := NewExpGolombStream()
 *
 * go eg.Decode(bytes, ints)
 * go func() {
 *          bytes <- 0x40
 * }()
 * for i := range ints {
 *          fmt.Println("Read an int:", i)
 * }
 * 
 * The channel interface is designed to make it easy to use in
 * streaming applications.
 *
 * At present, this code is not optimized for speed.
 */

package deltagolomb

type ExpGolombStream struct {
	data   byte
	bitpos uint
}

// Create a new Exp-Golomb stream encoder/decoder object.
func NewExpGolombStream() *ExpGolombStream {
	return &ExpGolombStream{0, 0}
}

// Decode states, bit-at-a-time (slow but safe)
const (
	COUNTING_ZEROS = iota
	SHIFTING_BITS
	READING_SIGN
)

// Encode a stream of signed integers into a byte stream.
// Reads all available ints from 'in';
// Emits encoded bytes to 'out'
func (s *ExpGolombStream) Encode(in chan int, out chan byte) {
	for i := range in {
		s.Add(i, out)
	}
	out <- s.data
	close(out)
	s.data = 0
}

// Decode a byte-stream of exp-golomb coded signed integers.
// Reads all available bytes from 'in';
// Emits decoded integers to 'out'.
func (s *ExpGolombStream) Decode(in chan byte, out chan int) {
	state := COUNTING_ZEROS
	val := 0
	zeros := 0
	for b := range in {
		for i := 7; i >= 0; i-- {
			bit := (b >> uint(i)) & 0x01
			switch state {
			case COUNTING_ZEROS:
				if bit == 0 {
					zeros++
				} else {
					if zeros == 0 {
						out <- 0
					} else {
						state = SHIFTING_BITS
						val = 1
					}
				}
			case SHIFTING_BITS:
				val <<= 1
				val |= int(bit)
				zeros--
				if zeros == 0 {
					val -= 1 // Because we stole bit for 0.
					state = READING_SIGN
				}
			case READING_SIGN:
				if bit == 1 {
					val = -val
				}
				out <- val
				state = COUNTING_ZEROS
			}
		}
	}
	// If we run off the end, do not emit the value.
	close(out)
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
// This function can be used if you don't want to use a channel
// interface for input and would prefer to call the Add
// function synchronously.
func (s *ExpGolombStream) Add(item int, out chan byte) {
	if item == 0 {
		s.addBit(1, out)
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
	//codelen := nbits * 2 + 1 + 1 // +1 for the separator, +1 for the sign bit.
	for i := uint(0); i < nbits; i++ {
		s.addBit(0, out)
	}
	s.addBit(1, out)
	for i := uint(1); i <= nbits; i++ {
		s.addBit((uitem>>(nbits-i))&0x01, out)
	}
	s.addBit(sign, out)
	return
}

// Helper function that adds one bit to our output byte stream.
// Emits the byte if it is full, otherwise just updates internal
// state.
func (s *ExpGolombStream) addBit(bit uint, out chan byte) {
	if s.bitpos == 8 {
		out <- s.data
		s.data = 0
		s.bitpos = 0
	}
	s.data |= (byte(1&bit) << (7 - s.bitpos))
	s.bitpos++
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

	result := NewExpGolombStream()
	intchan := make(chan int)
	bytestream := make(chan byte)

	go result.Encode(intchan, bytestream)

	go func() {
		prev := start
		for _, i := range data {
			delta := i - prev
			prev = i
			intchan <- delta
		}
		close(intchan)
	}()

	ret := make([]byte, 0)
	for b := range bytestream {
		ret = append(ret, b)
	}
	return ret
}

// Decodes an array of bytes representing an Exp-Golomb encoded
// stream of residuals of delta compression.  Returns the
// results as an array of integers.
func DeltaDecode(base int, compressed []byte) []int {
	res := make([]int, 0)
	c := make(chan int)
	bytechan := make(chan byte)
	val := base
	decoder := NewExpGolombStream()

	go func() {
		for _, b := range compressed {
			bytechan <- b
		}
		close(bytechan)
	}()

	go decoder.Decode(bytechan, c)

	for delta := range c {
		val += delta
		res = append(res, val)
	}
	return res
}
