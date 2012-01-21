package deltagolomb

import (
	"bytes"
	"io/ioutil"
	"testing"
)

type etest struct {
	ints  []int
	bytes []byte
}

var bytetests = []etest{
	{[]int{0}, []byte{0x80}},                         // 0b1000000
	{[]int{1}, []byte{0x40}},                         // 0b0100 0000
	{[]int{2}, []byte{0x60}},                         // 0b0110 0000
	{[]int{3}, []byte{0x20}},                         // 0b001000 00
	{[]int{6}, []byte{0x38}},                         // 0b001110 00
	{[]int{-6}, []byte{0x3c}},                        // 0b001111 00
	{[]int{0, 0}, []byte{0xc0}},                      // 0b1100 0000
	{[]int{6, 12}, []byte{0x38, 0xe0}},               // 0b0011 1000 1110 0000
	{[]int{65537}, []byte{0x0, 0x0, 0x80, 0x1, 0x0}}, // 0b 00000000 00000000 10000000 0000001 00 {00...}
}

func TestEncode(t *testing.T) {
	for _, bt := range bytetests {
		e := DeltaEncode(0, bt.ints)
		if bytes.Compare(bt.bytes, e) != 0 {
			t.Fatal("Encode of ", bt.ints, " failed, got ", e, " expected ", bt.bytes)
		}
	}
}

func TestEncodeDecode(t *testing.T) {
	o := make([]int, 25)
	base := 6329
	for delta := 0; delta < 257; delta++ {
		for i := 0; i < len(o); i++ {
			o[i] = base + i*delta
		}
		e := DeltaEncode(base, o)
		d := DeltaDecode(base, e)
		if len(d) != len(o) {
			t.Errorf("Len(d) = %d, want %d.", len(d), len(o))
		}
		for i := 0; i < len(o) && i < len(d); i++ {
			if d[i] != o[i] {
				t.Fatalf("For delta %d item %d mismatch.  Want %d got %d.", delta, i, o[i], d[i])
			}
		}
	}
}

func BenchmarkExpGEncode(b *testing.B) {
	b.StopTimer()

	egs := NewExpGolombEncoder(ioutil.Discard)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		egs.Write([]int{0, 1, -1, 2, -5})
	}
	egs.Close()
}

// Benchmarks decode speed.  Because it resets the buffer
// and does some other work, this test decodes 200 symbols
// per iteration, so divde the ns/op by 200 to find
// the per-symbol cost.
func BenchmarkExpGDecode(b *testing.B) {
	b.StopTimer()
	buf := &bytes.Buffer{}
	egs := NewExpGolombEncoder(buf)
	for i := 0; i < 40; i++ {
		egs.Write([]int{0, 1, -1, 2, -5})
	}
	egs.Close()

	bbytes := buf.Bytes()
	saved_b := make([]byte, len(bbytes))
	copy(saved_b, bbytes)

	b.StartTimer()
	res := make([]int, 200)
	for i := 0; i < b.N; i++ {
		decoder := NewExpGolombDecoder(buf)
		n, _ := decoder.Read(res)
		if n != 200 {
			b.Fatalf("Expected 200 ints, got %d", n)
		}
		buf.Reset()
		buf.Write(saved_b)
	}
}
