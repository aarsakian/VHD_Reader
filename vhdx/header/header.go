package header

import (
	"hash/crc32"
	"vhdxreader/utils"
)

// ---------------------------------------------------------------------
// VHDX Header – MS-VHDX 2.2.2
// Two copies exist; you pick the one with highest SequenceNumber.
// ---------------------------------------------------------------------

type VHDXHeader struct {
	Signature      [4]byte // "head"
	Checksum       uint32
	SequenceNumber uint64
	FileWriteGuid  [16]byte
	DataWriteGuid  [16]byte
	LogGuid        [16]byte
	LogVersion     uint16
	Version        uint16
	LogLength      uint32
	LogOffset      uint64
	Reserved       [4016]byte
}

func (h *VHDXHeader) IsValid(data []byte) bool {
	data[4] = 0 // zero out checksum for validation
	data[5] = 0
	data[6] = 0
	data[7] = 0

	return string(h.Signature[:]) == "head" &&

		h.Checksum == crc32.Checksum(data, utils.CrC32Table)
}

func (h *VHDXHeader) Parse(data []byte) error {
	_, err := utils.Unmarshal(data, h)
	if err != nil {
		return err
	}

	return nil
}
