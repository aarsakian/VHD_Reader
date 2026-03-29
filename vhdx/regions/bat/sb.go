package blocks

import (
	"fmt"
	"vhdxreader/utils"
)

var sectorStateMap = map[int]string{
	6: "Present",
	7: "Not Present",
}

// SB blocks exist to support partial‑block writes in differencing disks.
type SectorBlock struct {
	State        uint8  `bits:"3"`  // bits 0–2:  payload state
	Reserved     uint32 `bits:"17"` // bits 3–19: reserved
	FileOffsetMB uint64 `bits:"44"` // bits 20–63: file offset in MB units
}

func (sb SectorBlock) GetFileOffset() uint64 {
	return sb.FileOffsetMB
}

func (sb SectorBlock) IsSectorBitmap() bool {
	return true
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

func (sb *SectorBlock) GetState() string {
	if s, ok := sectorStateMap[int(sb.State)]; ok {
		return s
	}
	return "Unknown"
}
