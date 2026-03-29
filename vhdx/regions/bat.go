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
	var entry BATEntry
	entryCount := len(data) / 8
	bat.Entries = make([]BATEntry, 0, entryCount)

	k := 0
	for i := range entryCount {

		if k == chunkRatio {
			entry = &blocks.SectorBlock{}
			k = 0
		} else {
			entry = &blocks.PayloadBlock{}
			k++
		}
		entry.Parse(data[i*8 : (i+1)*8])

		bat.Entries = append(bat.Entries, entry)
	}
	return nil
}

func (bat BAT) ShowInfo() {
	for i, entry := range bat.Entries {

		fmt.Printf("BAT Entry %d: State=%s\n", i+1, entry.GetState())
	}
}
