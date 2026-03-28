package regions

import (
	"fmt"
	"vhdxreader/utils"
)

// ---------------------------------------------------------------------
// BAT (Block Allocation Table) – MS-VHDX 2.2.5
// Each entry is 64 bits.
// ---------------------------------------------------------------------

type BAT []*BATEntry

type BATEntry struct {
	State        uint8  `bits:"3"`  // bits 0–2:  payload state
	Reserved     uint32 `bits:"17"` // bits 3–19: reserved
	FileOffsetMB uint64 `bits:"44"` // bits 20–63: file offset in MB units
}

func (bat *BAT) Parse(data []byte) error {

	entryCount := len(data) / 8
	*bat = make(BAT, 0, entryCount)
	for i := range entryCount {
		entry := new(BATEntry)
		utils.Unmarshal(data[i*8:(i+1)*8], entry)
		*bat = append(*bat, entry)
	}
	return nil
}

func (bat BAT) ShowInfo() {
	for i, entry := range bat {
		state := entry.State // already 3 bits
		var stateStr string
		switch state {
		case 0:
			stateStr = "Unallocated"
		case 1:
			stateStr = "Allocated"
		case 2:
			stateStr = "Zeroed"
		case 3:
			stateStr = "Unmapped"
		case 6:
			stateStr = "Fully Present"
		case 7:
			stateStr = "Partially Present"
		default:
			stateStr = "Reserved"
		}
		fmt.Printf("BAT Entry %d: State=%s\n", i+1, stateStr)
	}
}
