package wav

import (
	"io"
)

const (
	AudioFormatPCM       = 1
	AudioFormatIEEEFloat = 3
	AudioFormatALaw      = 6
	AudioFormatMULaw     = 7
)

type WavFormat struct {
	AudioFormat   uint16
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

type WavData struct {
	// Original io.Reader, which will be a *bufio.Reader.
	// We keep this to allow oto to read from it.
	internalReader io.Reader 
	Size uint32
	Position  uint32 // Exported to track read position
}

// Read implements the io.Reader interface for WavData.
// This is where we'll update the Position.
func (wd *WavData) Read(p []byte) (n int, err error) {
	n, err = wd.internalReader.Read(p)
	wd.Position += uint32(n) // Update the position after reading
	return n, err
}

type Sample struct {
	Values [2]int
}
