package blocks

import (
	"fmt"
	"vhdxreader/utils"
)

var payloadStateMap = map[int]string{
	0: "Unallocated",
	1: "Allocated",
	2: "Zeroed",
	3: "Unmapped",
	6: "Fully Present",
	7: "Partially Present",
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

func (pb *PayloadBlock) GetState() string {
	if s, ok := payloadStateMap[int(pb.State)]; ok {
		return s
	}
	return "Unknown"
}

func (pb PayloadBlock) GetFileOffset() uint64 {
	return pb.FileOffsetMB
}

func (pb PayloadBlock) IsSectorBitmap() bool {
	return false
}
