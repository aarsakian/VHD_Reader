package regions

import (
	"fmt"
	"vhdxreader/utils"
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
	GetState() uint8
}

var payloadStateMap = map[int]string{
	0: "Unallocated",
	1: "Allocated",
	2: "Zeroed",
	3: "Unmapped",
	6: "Fully Present",
	7: "Partially Present",
}

var sectorStateMap = map[int]string{
	6: "Present",
	7: "Not Present",
}

type PayloadBlock struct {
	State        uint8  `bits:"3"`  // bits 0–2:  payload state
	Reserved     uint32 `bits:"17"` // bits 3–19: reserved
	FileOffsetMB uint64 `bits:"44"` // bits 20–63: file offset in MB units
}

// each block covers a chunk of data, and the chunk size is determined by the chunk ratio (e.g., 1MB, 2MB, etc.) specified in the metadata. The BAT entries indicate whether the corresponding chunk is allocated, unallocated, zeroed, or unmapped.
//
//	For differencing disks, some entries may indicate partial block states to support partial-block writes.
func (pb *PayloadBlock) Parse(data []byte) error {
	if len(data) != 8 {
		return fmt.Errorf("invalid BAT entry length: expected 8 bytes, got %d", len(data))
	}
	_, err := utils.Unmarshal(data, pb)
	if err != nil {
		return fmt.Errorf("failed to parse PayloadBlock: %v", err)
	}

	return nil
}

func (pb *PayloadBlock) GetState() uint8 {
	return pb.State
}

// SB blocks exist to support partial‑block writes in differencing disks.
type SectorBlock struct {
	State        uint8  `bits:"3"`  // bits 0–2:  payload state
	Reserved     uint32 `bits:"17"` // bits 3–19: reserved
	FileOffsetMB uint64 `bits:"44"` // bits 20–63: file offset in MB units
}

func (sb *SectorBlock) Parse(data []byte) error {
	if len(data) != 8 {
		return fmt.Errorf("invalid BAT entry length: expected 8 bytes, got %d", len(data))
	}
	_, err := utils.Unmarshal(data, sb)
	if err != nil {
		return fmt.Errorf("failed to parse SectorBlock: %v", err)
	}

	return nil
}

func (sb *SectorBlock) GetState() uint8 {
	return sb.State
}

func (bat *BAT) Parse(data []byte, chunkRatio int) error {
	var entry BATEntry
	entryCount := len(data) / 8
	bat.Entries = make([]BATEntry, 0, entryCount)

	k := 0
	for i := range entryCount {

		if k == chunkRatio {
			entry = &SectorBlock{}
			k = 0
		} else {
			entry = &PayloadBlock{}
			k++
		}
		entry.Parse(data[i*8 : (i+1)*8])

		bat.Entries = append(bat.Entries, entry)
	}
	return nil
}

func (bat BAT) ShowInfo() {
	for i, entry := range bat.Entries {
		state := int(entry.GetState())
		stateStr := "Reserved"

		switch entry.(type) {
		case *SectorBlock:
			if s, ok := sectorStateMap[state]; ok {
				stateStr = s
			}
		default:
			if s, ok := payloadStateMap[state]; ok {
				stateStr = s
			}
		}

		fmt.Printf("BAT Entry %d: State=%s\n", i+1, stateStr)
	}
}
