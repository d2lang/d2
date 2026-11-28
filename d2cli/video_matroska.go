package d2cli

import (
	"encoding/binary"
	"image"
	"math/bits"
)

// FFmpeg's rawvideo demuxer assigns one tick to every input picture. A single
// uncompressed Matroska track lets us send those same RGBA bytes with an
// explicit timestamp and duration. These are just the streaming elements we
// need from the Matroska element specification.
func videoTrackHeader(size image.Point) []byte {
	header := ebmlElement(0x1a45dfa3, // EBML header
		ebmlElement(0x4282, []byte("matroska")), // DocType
		ebmlElement(0x4287, []byte{4}),          // DocTypeVersion
		ebmlElement(0x4285, []byte{2}),          // DocTypeReadVersion
	)
	// A segment of unknown length needs neither a seek nor an index.
	header = append(header, 0x18, 0x53, 0x80, 0x67, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	info := ebmlElement(0x1549a966, // Info
		ebmlElement(0x2ad7b1, ebmlUint(1_000_000)), // TimestampScale: one millisecond in ns.
		ebmlElement(0x4d80, []byte("D2")),          // MuxingApp
		ebmlElement(0x5741, []byte("D2")),          // WritingApp
	)
	track := ebmlElement(0xae, // TrackEntry
		ebmlElement(0xd7, []byte{1}),                            // TrackNumber
		ebmlElement(0x73c5, []byte{1}),                          // TrackUID
		ebmlElement(0x83, []byte{1}),                            // Video TrackType
		ebmlElement(0x86, []byte("V_UNCOMPRESSED")),             // CodecID
		ebmlElement(0x23e383, ebmlUint(1_000_000_000/videoFPS)), // DefaultDuration, ns
		ebmlElement(0xe0, // Video
			ebmlElement(0xb0, ebmlUint(uint64(size.X))),
			ebmlElement(0xba, ebmlUint(uint64(size.Y))),
			ebmlElement(0x2eb524, []byte("RGBA")), // UncompressedFourCC
		),
	)
	header = append(header, info...)
	return append(header, ebmlElement(0x1654ae6b, track)...) // Tracks
}

// One cluster per distinct picture keeps the block's signed 16-bit relative
// timestamp at zero, even across minute-long holds. The pixel payload follows
// the returned prefix directly on the pipe and is never copied into it.
func videoBlockHeader(tick, repeats, pixelBytes int) []byte {
	milliseconds := func(frame int) uint64 {
		return (uint64(frame)*1000 + videoFPS/2) / videoFPS
	}
	start := milliseconds(tick)
	duration := milliseconds(tick+repeats) - start
	block := ebmlPrefix(0xa1, pixelBytes, []byte{0x81, 0, 0, 0}) // Track 1, relative time 0, no lacing.
	group := ebmlPrefix(0xa0, pixelBytes, ebmlElement(0x9b, ebmlUint(duration)), block)
	return ebmlPrefix(0x1f43b675, pixelBytes, ebmlElement(0xe7, ebmlUint(start)), group)
}

func ebmlElement(id uint32, fields ...[]byte) []byte {
	return ebmlPrefix(id, 0, fields...)
}

// IDs and unsigned values are big-endian. The leading set bit of a size
// encodes its byte count; all-one values at any width are reserved.
func ebmlPrefix(id uint32, followingBytes int, fields ...[]byte) []byte {
	size := followingBytes
	for _, field := range fields {
		size += len(field)
	}
	width := max(1, (bits.Len(uint(size+1))+6)/7)
	header := ebmlUint(uint64(id))
	header = append(header, ebmlUint(uint64(size)|1<<(7*width))...)
	for _, field := range fields {
		header = append(header, field...)
	}
	return header
}

func ebmlUint(value uint64) []byte {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	return data[8-max(1, (bits.Len64(value)+7)/8):]
}
