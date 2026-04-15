package regions

import (
	"fmt"
	blocks "vhdxreader/vhdx/regions/bat"
)

// ---------------------------------------------------------------------
// BAT (Block Allocation Table) – MS-VHDX 2.2.5
// Each entry is 64 bits.
// ---------------------------------------------------------------------

type BAT struct {
	Entries []BATEntry
}

type BATEntry interface {
	Parse(data []byte) error
	GetState() string
	GetFileOffset() uint64
	IsSectorBitmap() bool
}

func (bat *BAT) Parse(data []byte, chunkRatio int) error {
	entryCount := len(data) / 8
	bat.Entries = make([]BATEntry, entryCount)

	chunkSize := chunkRatio + 1 // PB...PB + SB

	for i := range entryCount {

		offsetInChunk := i % chunkSize

		var entry BATEntry

		if offsetInChunk == chunkRatio {
			entry = &blocks.SectorBlock{} // SB
		} else {
			entry = &blocks.PayloadBlock{} // PB
		}

		entry.Parse(data[i*8 : (i+1)*8])
		bat.Entries[i] = entry
	}

	return nil
}

func (bat BAT) ShowInfo() {
	for i, entry := range bat.Entries {

		fmt.Printf("BAT Entry %d: State=%s\n", i+1, entry.GetState())
	}
}
