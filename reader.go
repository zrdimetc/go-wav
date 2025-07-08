package wav

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"time"

	"github.com/youpy/go-riff"
	"github.com/zaf/g711"
)

type Reader struct {
	r         *riff.Reader
	riffChunk *riff.RIFFChunk
	format    *WavFormat
	*WavData // Embed WavData to access its fields
}

func NewReader(r riff.RIFFReader) *Reader {
	riffReader := riff.NewReader(r)
	return &Reader{r: riffReader}
}

func (r *Reader) Format() (format *WavFormat, err error) {
	if r.format == nil {
		format, err = r.readFormat()
		if err != nil {
			return
		}
		r.format = format
	} else {
		format = r.format
	}

	return
}

// Duration calculates the total duration of the audio in the WAV file.
// It relies on the size of the 'data' chunk, the block alignment, and the sample rate.
func (r *Reader) Duration() (time.Duration, error) {
	format, err := r.Format()
	if err != nil {
		return 0.0, err
	}

	err = r.loadWavData()
	if err != nil {
		return 0.0, err
	}

	// Calculate the total number of samples from the data chunk size.
	totalSamples := float64(r.WavData.Size) / float64(format.BlockAlign)

	// Calculate duration in seconds.
	sec := totalSamples / float64(format.SampleRate)

	return time.Duration(sec*1000000000) * time.Nanosecond, nil
}

// Read reads bytes from the WAV data stream into the provided byte slice p.
// It ensures the underlying WavData is loaded before reading.
// This method is called by oto.Player.
func (r *Reader) Read(p []byte) (n int, err error) {
	err = r.loadWavData()
	if err != nil {
		return n, err
	}

	// This now calls the Read method of the embedded WavData,
	// which in turn calls its internalReader and updates its Position.
	return r.WavData.Read(p)
}

// GetCurrentPosition returns the current read position in bytes within the data chunk.
func (r *Reader) GetCurrentPosition() (uint32, error) {
	if r.WavData == nil {
		return 0, errors.New("WavData not loaded yet")
	}
	return r.WavData.Position, nil
}

// ReadSamples reads a specified number of samples (or a default if not specified)
// from the WAV data stream. It handles different audio formats.
func (r *Reader) ReadSamples(params ...uint32) (samples []Sample, err error) {
	var bytesToRead int
	var numSamples int

	format, err := r.Format()
	if err != nil {
		return
	}

	numChannels := int(format.NumChannels)
	blockAlign := int(format.BlockAlign)
	bitsPerSample := int(format.BitsPerSample)

	if len(params) > 0 {
		numSamples = int(params[0])
	} else {
		numSamples = 2048
	}

	bytesToRead = numSamples * blockAlign
	bytes := make([]byte, bytesToRead)
	n, err := r.Read(bytes) // This calls r.Read, which then calls r.WavData.Read (embedded io.Reader)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n == 0 && err == io.EOF {
		return nil, io.EOF
	}

	samples = make([]Sample, numSamples)
	offset := 0

	for i := 0; i < numSamples; i++ {
		for j := 0; j < numChannels; j++ {
			soffset := offset + (j * bitsPerSample / 8)

			switch format.AudioFormat {
			case AudioFormatIEEEFloat:
				if soffset+3 >= n { // Check bounds against n, actual bytes read
					err = io.EOF
					return
				}
				bits :=
					uint32((int(bytes[soffset+3]) << 24) +
						(int(bytes[soffset+2]) << 16) +
						(int(bytes[soffset+1]) << 8) +
						int(bytes[soffset]))
				samples[i].Values[j] = int(math.MaxInt32 * math.Float32frombits(bits))

			case AudioFormatALaw:
				if soffset >= n { // Check bounds against n
					err = io.EOF
					return
				}
				pcm := g711.DecodeAlawFrame(bytes[soffset]) // Corrected to DecodeAlawFrame
				samples[i].Values[j] = int(pcm)

			case AudioFormatMULaw:
				if soffset >= n { // Check bounds against n
					err = io.EOF
					return
				}
				pcm := g711.DecodeUlawFrame(bytes[soffset]) // Corrected to DecodeUlawFrame
				samples[i].Values[j] = int(pcm)

			default:
				var val uint
				bytesForSample := bitsPerSample / 8
				if soffset+bytesForSample > n { // Check bounds against n
					err = io.EOF
					return
				}
				for b_idx := 0; b_idx < bytesForSample; b_idx++ {
					val += uint(bytes[soffset+b_idx]) << uint(b_idx*8)
				}
				samples[i].Values[j] = toInt(val, bitsPerSample)
			}
		}

		offset += blockAlign
	}

	return
}

func (r *Reader) IntValue(sample Sample, channel uint) int {
	return sample.Values[channel]
}

func (r *Reader) FloatValue(sample Sample, channel uint) float64 {
	if r.format.BitsPerSample == 0 {
		return 0.0
	}
	return float64(r.IntValue(sample, channel)) / math.Pow(2, float64(r.format.BitsPerSample-1))
}

func (r *Reader) readFormat() (fmt *WavFormat, err error) {
	var riffChunk *riff.RIFFChunk

	fmt = new(WavFormat)

	if r.riffChunk == nil {
		riffChunk, err = r.r.Read()
		if err != nil {
			return
		}

		r.riffChunk = riffChunk
	} else {
		riffChunk = r.riffChunk
	}

	fmtChunk := findChunk(riffChunk, "fmt ")

	if fmtChunk == nil {
		err = errors.New("format chunk is not found")
		return
	}

	err = binary.Read(fmtChunk, binary.LittleEndian, fmt)
	if err != nil {
		return
	}

	if fmt.BitsPerSample == 0 {
		return nil, errors.New("BitsPerSample is 0, which is invalid for audio format")
	}

	return
}

func (r *Reader) loadWavData() error {
	if r.WavData == nil {
		data, err := r.readData()
		if err != nil {
			return err
		}
		r.WavData = data
	}

	return nil
}

func (r *Reader) readData() (data *WavData, err error) {
	var riffChunk *riff.RIFFChunk

	if r.riffChunk == nil {
		riffChunk, err = r.r.Read()
		if err != nil {
			return
		}

		r.riffChunk = riffChunk
	} else {
		riffChunk = r.riffChunk
	}

	dataChunk := findChunk(riffChunk, "data")
	if dataChunk == nil {
		err = errors.New("data chunk is not found")
		return
	}

	// Initialize WavData with the internalReader set to bufio.NewReader(dataChunk)
	data = &WavData{internalReader: bufio.NewReader(dataChunk), Size: dataChunk.ChunkSize, Position: 0}

	return
}

func findChunk(riffChunk *riff.RIFFChunk, id string) (chunk *riff.Chunk) {
	for _, ch := range riffChunk.Chunks {
		if string(ch.ChunkID[:]) == id {
			chunk = ch
			break
		}
	}

	return
}

func toInt(value uint, bits int) int {
	var result int

	switch bits {
	case 32:
		result = int(int32(value))
	case 16:
		result = int(int16(value))
	case 8:
		result = int(int8(value))
	default:
		msb := uint(1 << (uint(bits) - 1))

		if value >= msb {
			result = -int((1 << uint(bits)) - value)
		} else {
			result = int(value)
		}
	}

	return result
}
