package regions

import (
	"fmt"
	"hash/crc32"

	"github.com/aarsakian/VHDXReader/utils"
)

// Known region GUIDs (you can fill actual values from spec)
var (
	// Metadata Region GUID: 8B7CA206-4790-4B9A-B8FE-575F050F886E
	RegionGuidMetadata = [16]byte{0x06, 0xA2, 0x7C, 0x8B, 0x90, 0x47, 0x9A, 0x4B, 0xB8, 0xFE, 0x57, 0x5F, 0x05, 0x0F, 0x88, 0x6E}
	// BAT Region GUID: 2DC27766-F623-4200-9D64-115E9BFD4A08
	RegionGuidBAT = [16]byte{0x66, 0x77, 0xC2, 0x2D, 0x23, 0xF6, 0x00, 0x42, 0x9D, 0x64, 0x11, 0x5E, 0x9B, 0xFD, 0x4A, 0x08}
)

// ---------------------------------------------------------------------
// Region Table – MS-VHDX 2.2.3
// ---------------------------------------------------------------------

type Regions []RegionTableEntry

type RegionTableHeader struct {
	Signature  [4]byte // "regi"
	Checksum   uint32
	EntryCount uint32
	Reserved   uint32
}

func (h *RegionTableHeader) IsValid(data []byte) bool {
	data[4] = 0
	data[5] = 0
	data[6] = 0
	data[7] = 0
	return string(h.Signature[:]) == "regi" &&
		h.Checksum == crc32.Checksum(data, utils.CrC32Table)
}

type RegionTableEntry struct {
	Guid       [16]byte
	FileOffset uint64
	Length     uint32
	Flags      uint32 // bit 0: Required
}

func (regions Regions) ShowInfo() {
	for i, r := range regions {
		fmt.Printf("Region %d:\n", i+1)
		fmt.Printf("  GUID: %s\n", utils.StringifyGUID(r.Guid[:]))
		fmt.Printf("  File Offset: %d bytes\n", r.FileOffset)
		fmt.Printf("  Length: %d bytes\n", r.Length)
		fmt.Printf("  Flags: %08b\n", r.Flags)
	}
}
