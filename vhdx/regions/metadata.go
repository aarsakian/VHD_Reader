package regions

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/aarsakian/VHD_Reader/utils"
)

// ---------------------------------------------------------------------
// Metadata Table – MS-VHDX 2.2.4
// ---------------------------------------------------------------------

// Known metadata item GUIDs (MS-VHDX 2.6.2), encoded in on-disk byte order.
var (
	// File Parameters: CAA16737-FA36-4D43-B3B6-33F0AA44E76B
	MetadataItemFileParameters = [16]byte{0x37, 0x67, 0xA1, 0xCA, 0x36, 0xFA, 0x43, 0x4D, 0xB3, 0xB6, 0x33, 0xF0, 0xAA, 0x44, 0xE7, 0x6B}
	// Virtual Disk Size: 2FA54224-CD1B-4876-B211-5DBED83BF4B8
	MetadataItemVirtualDiskSize = [16]byte{0x24, 0x42, 0xA5, 0x2F, 0x1B, 0xCD, 0x76, 0x48, 0xB2, 0x11, 0x5D, 0xBE, 0xD8, 0x3B, 0xF4, 0xB8}
	// Page 83 Data: BECA12AB-B2E6-4523-93EF-C309E000C746
	MetadataItemVirtualDiskId = [16]byte{0xAB, 0x12, 0xCA, 0xBE, 0xE6, 0xB2, 0x23, 0x45, 0x93, 0xEF, 0xC3, 0x09, 0xE0, 0x00, 0xC7, 0x46}
	// Logical Sector Size: 8141BF1D-A96F-4709-BA47-F233A8FAAB5F
	MetadataItemLogicalSectorSize = [16]byte{0x1D, 0xBF, 0x41, 0x81, 0x6F, 0xA9, 0x09, 0x47, 0xBA, 0x47, 0xF2, 0x33, 0xA8, 0xFA, 0xAB, 0x5F}
	// Physical Sector Size: CDA348C7-445D-4471-9CC9-E9885251C556
	MetadataItemPhysicalSectorSize = [16]byte{0xC7, 0x48, 0xA3, 0xCD, 0x5D, 0x44, 0x71, 0x44, 0x9C, 0xC9, 0xE9, 0x88, 0x52, 0x51, 0xC5, 0x56}
	// Parent Locator: A8D35F2D-B30B-454D-ABF7-D3D84834AB0C
	MetadataItemParentLocator = [16]byte{0x2D, 0x5F, 0xD3, 0xA8, 0x0B, 0xB3, 0x4D, 0x45, 0xAB, 0xF7, 0xD3, 0xD8, 0x48, 0x34, 0xAB, 0x0C}
)

var metadataFileParametersFlagNames = map[uint32]string{
	1 << 0: "LeaveBlocksAllocated",
	1 << 1: "HasParent",
}

var metadataItemNames = map[[16]byte]string{
	MetadataItemFileParameters:     "File Parameters",
	MetadataItemVirtualDiskSize:    "Virtual Disk Size",
	MetadataItemVirtualDiskId:      "Virtual Disk ID",
	MetadataItemLogicalSectorSize:  "Logical Sector Size",
	MetadataItemPhysicalSectorSize: "Physical Sector Size",
	MetadataItemParentLocator:      "Parent Locator",
}

type Metadata struct {
	Table   *MetadataTableHeader
	Entries []*MetadataTableEntry
}

type MetadataTableHeader struct {
	Signature  [8]byte  // "metadata"
	Reserved   uint16   // must be zero
	EntryCount uint16   // number of metadata entries
	Reserved2  [20]byte // must be zero

}

type MetadataTableEntry struct {
	ItemID             [16]byte //guid of the metadata item
	Offset             uint32   // Offset from start of metadata region
	Length             uint32
	Flags              uint32 // bit 0: IsUser, bit 1: IsVirtualDisk, bit 2: IsRequired
	Reserved           uint32
	FileParameters     *FileParameters
	VirtualDiskSize    *VirtualDiskSize
	LogicalSectorSize  *LogicalSectorSize
	PhysicalSectorSize *PhysicalSectorSize
	ParentLocator      *ParentLocator
	VirtualDiskId      *VirtualDiskId
}

// ---------------------------------------------------------------------
// File Parameters – MS-VHDX 2.2.4.1
// ---------------------------------------------------------------------

type FileParameters struct {
	BlockSize            uint32
	RawFlags             uint32 // store original flags
	LeaveBlocksAllocated bool   // derived from Flags
	HasParent            bool   // derived from Flags

}

// ---------------------------------------------------------------------
// Virtual Disk Size – MS-VHDX 2.2.4.2
// ---------------------------------------------------------------------

type VirtualDiskSize struct {
	Size uint64
}

// ---------------------------------------------------------------------
// Logical Sector Size – MS-VHDX 2.2.4.3
// ---------------------------------------------------------------------

type LogicalSectorSize struct {
	Size uint32
}

// ---------------------------------------------------------------------
// Physical Sector Size – MS-VHDX 2.2.4.4
// ---------------------------------------------------------------------

type PhysicalSectorSize struct {
	Size uint32
}

// ---------------------------------------------------------------------
// Block Size – MS-VHDX 2.2.4.5
// ---------------------------------------------------------------------

type BlockSize struct {
	Size uint32
}

// ---------------------------------------------------------------------
// Parent Locator – MS-VHDX 2.6.3
// ---------------------------------------------------------------------

// VHDX Parent Locator type GUID: B04AEFB7-D19E-4A81-B789-25B8E9445913
var ParentLocatorTypeVHDX = [16]byte{0xB7, 0xEF, 0x4A, 0xB0, 0x9E, 0xD1, 0x81, 0x4A, 0xB7, 0x89, 0x25, 0xB8, 0xE9, 0x44, 0x59, 0x13}

type ParentLocator struct {
	Header   *ParentLocatorHeader
	Entries  []*ParentLocatorEntry
	Locators map[string]string // key-value pairs extracted from entries
}

// ParentLocatorHeader is the fixed-size header of a Parent Locator metadata item.
type ParentLocatorHeader struct {
	LocatorType   [16]byte // must equal ParentLocatorTypeVHDX
	Reserved      uint16
	KeyValueCount uint16 // number of ParentLocatorEntry records that follow
}

// ParentLocatorEntry is one key/value pair inside the Parent Locator item.
// KeyOffset and ValueOffset are relative to the start of the metadata item.
type ParentLocatorEntry struct {
	KeyOffset   uint32
	ValueOffset uint32
	KeyLength   uint16
	ValueLength uint16
}

type VirtualDiskId struct {
	Id [16]byte
}

func (mt Metadata) HasParent() bool {
	for _, entry := range mt.Entries {
		if entry.FileParameters != nil && entry.FileParameters.HasParent {
			return true
		}
	}
	return false
}

// IsRequired reports whether the implementation must understand this item to load the file.
func (e *MetadataTableEntry) IsRequired() bool {
	return e.Flags&(1<<2) != 0
}

// IsUser reports whether this is user metadata (as opposed to system metadata).
func (e *MetadataTableEntry) IsUser() bool {
	return e.Flags&(1<<0) != 0
}

// IsVirtualDisk reports whether this is virtual-disk metadata (vs file metadata).
func (e *MetadataTableEntry) IsVirtualDisk() bool {
	return e.Flags&(1<<1) != 0
}

func (mt *Metadata) Parse(handler *os.File, metadataOffset uint64) error {
	buf := make([]byte, 64*1024)
	handler.ReadAt(buf, int64(metadataOffset)) // read metadata region (assuming it's the first one for simplicity	)

	if len(buf) < 16 {
		return fmt.Errorf("metadata region too small")
	}
	mh := new(MetadataTableHeader)
	_, err := utils.Unmarshal(buf, mh)
	if err != nil {
		return fmt.Errorf("failed to parse metadata header: %v", err)
	}
	mt.Table = mh

	offset := 32

	mt.Entries = make([]*MetadataTableEntry, 0, mh.EntryCount)
	for i := 0; i < int(mh.EntryCount); i++ {
		if offset+32 > len(buf) {
			return fmt.Errorf("metadata region too small for entry %d", i)
		}
		entry := new(MetadataTableEntry)
		_, err := utils.Unmarshal(buf[offset:offset+32], entry)
		if err != nil {
			return fmt.Errorf("failed to unmarshal metadata entry: %v", err)
		}
		mt.Entries = append(mt.Entries, entry)
		offset += 32
	}

	for _, entry := range mt.Entries {
		if err := entry.AddKnownMetadata(handler, metadataOffset); err != nil {
			return fmt.Errorf("failed to add known metadata: %v", err)
		}

	}
	return nil

}

func (e *MetadataTableEntry) AddKnownMetadata(handler *os.File, metadataOffset uint64) error {
	if !e.IsUser() && !e.IsVirtualDisk() && e.IsRequired() &&
		e.IsFileParameters() {
		// Parse File Parameters item
		buf := make([]byte, e.Length)
		handler.ReadAt(buf, int64(metadataOffset)+int64(e.Offset)) // read the specific metadata item
		fp := new(FileParameters)
		_, err := utils.Unmarshal(buf, fp)
		if err != nil {
			return err
		}
		fp.LeaveBlocksAllocated = fp.RawFlags&(1<<0) != 0
		fp.HasParent = fp.RawFlags&(1<<1) != 0
		e.FileParameters = fp

	} else if !e.IsUser() && e.IsVirtualDisk() && e.IsRequired() &&
		e.IsVirtualDiskSize() {
		// Parse Virtual Disk Size item
		buf := make([]byte, e.Length)
		handler.ReadAt(buf, int64(metadataOffset)+int64(e.Offset)) // read the specific metadata item
		vds := new(VirtualDiskSize)
		_, err := utils.Unmarshal(buf, vds)
		if err != nil {
			return err
		}
		e.VirtualDiskSize = vds
	} else if !e.IsUser() && e.IsVirtualDisk() && e.IsRequired() &&
		e.IsLogicalSectorSize() {
		// Parse Logical Sector Size item
		buf := make([]byte, e.Length)
		handler.ReadAt(buf, int64(metadataOffset)+int64(e.Offset)) // read the specific metadata item
		lss := new(LogicalSectorSize)
		_, err := utils.Unmarshal(buf, lss)
		if err != nil {
			return err
		}
		e.LogicalSectorSize = lss
	} else if !e.IsUser() && e.IsVirtualDisk() && e.IsRequired() &&
		e.IsPhysicalSectorSize() {
		// Parse Physical Sector Size item
		buf := make([]byte, e.Length)
		handler.ReadAt(buf, int64(metadataOffset)+int64(e.Offset)) // read the specific metadata item
		pss := new(PhysicalSectorSize)
		_, err := utils.Unmarshal(buf, pss)
		if err != nil {
			return err
		}
		e.PhysicalSectorSize = pss
	} else if !e.IsUser() && !e.IsVirtualDisk() && e.IsRequired() &&
		e.IsParentLocator() {
		// Parse Parent Locator item
		pl := new(ParentLocator)
		buf := make([]byte, e.Length)
		handler.ReadAt(buf, int64(metadataOffset)+int64(e.Offset)) // read the specific metadata item
		err := pl.Parse(buf)
		pl.GetLocators(buf)

		if err != nil {
			return err
		}

		e.ParentLocator = pl
	} else if !e.IsUser() && e.IsVirtualDisk() && e.IsRequired() &&
		e.IsVirtualDiskId() {
		// Parse Virtual Disk Id item
		buf := make([]byte, e.Length)
		handler.ReadAt(buf, int64(metadataOffset)+int64(e.Offset))
		vdi := new(VirtualDiskId)
		_, err := utils.Unmarshal(buf, vdi)
		if err != nil {
			return err
		}
		e.VirtualDiskId = vdi
	}

	return nil
}

func (pl *ParentLocator) Parse(buf []byte) error {
	pl.Header = new(ParentLocatorHeader)
	_, err := utils.Unmarshal(buf, pl.Header)
	if err != nil {
		return err
	}

	offset := 20 // start of entries
	pl.Entries = make([]*ParentLocatorEntry, pl.Header.KeyValueCount)
	for i := 0; i < int(pl.Header.KeyValueCount); i++ {

		if offset+12 > len(buf) {
			return fmt.Errorf("metadata item too small for parent locator entry %d", i)
		}
		entry := new(ParentLocatorEntry)
		_, err := utils.Unmarshal(buf[offset:offset+12], entry)
		if err != nil {
			return fmt.Errorf("failed to unmarshal parent locator entry: %v", err)
		}
		pl.Entries[i] = entry
		offset += 12
	}

	return nil
}

func (pl *ParentLocator) GetLocators(buf []byte) {
	pl.Locators = make(map[string]string, len(pl.Entries))
	for _, entry := range pl.Entries {

		key := utils.DecodeUTF16(buf[entry.KeyOffset : entry.KeyOffset+uint32(entry.KeyLength)])
		value := utils.DecodeUTF16(buf[entry.ValueOffset : entry.ValueOffset+uint32(entry.ValueLength)])
		pl.Locators[key] = value
	}

}

func (e MetadataTableEntry) IsFileParameters() bool {
	return e.ItemID == MetadataItemFileParameters
}

func (e MetadataTableEntry) IsVirtualDiskSize() bool {
	return e.ItemID == MetadataItemVirtualDiskSize
}

func (e MetadataTableEntry) IsVirtualDiskId() bool {
	return e.ItemID == MetadataItemVirtualDiskId
}

func (e MetadataTableEntry) IsLogicalSectorSize() bool {
	return e.ItemID == MetadataItemLogicalSectorSize
}

func (e MetadataTableEntry) IsPhysicalSectorSize() bool {
	return e.ItemID == MetadataItemPhysicalSectorSize
}

func (e MetadataTableEntry) IsParentLocator() bool {
	return e.ItemID == MetadataItemParentLocator
}

func metadataFlagsToString(rawFlags uint32, labels map[uint32]string) string {
	if rawFlags == 0 {
		return "none"
	}

	bits := make([]uint32, 0, len(labels))
	for bit := range labels {
		bits = append(bits, bit)
	}
	sort.Slice(bits, func(i, j int) bool { return bits[i] < bits[j] })

	parts := make([]string, 0, len(bits))
	for _, bit := range bits {
		if rawFlags&bit != 0 {
			parts = append(parts, labels[bit])
		}
	}

	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func (mt Metadata) ShowInfo() {
	fmt.Printf("Metadata Table:\n")
	fmt.Printf("  Signature: %s\n", string(mt.Table.Signature[:]))
	fmt.Printf("  Entry Count: %d\n", mt.Table.EntryCount)
	for i, entry := range mt.Entries {
		name, ok := metadataItemNames[entry.ItemID]
		if !ok {
			name = "Unknown"
		}

		fmt.Printf("  Entry %d:\n", i+1)
		fmt.Printf("    Item: %s\n", name)

		fmt.Printf("    Flags: user=%t virtualDisk=%t required=%t\n", entry.IsUser(), entry.IsVirtualDisk(), entry.IsRequired())

		if entry.FileParameters != nil {
			fmt.Printf("    File Parameters: blockSize=%d rawFlags=0x%08X flags=%s\n",
				entry.FileParameters.BlockSize,
				entry.FileParameters.RawFlags,
				metadataFlagsToString(entry.FileParameters.RawFlags, metadataFileParametersFlagNames),
			)
		}
		if entry.VirtualDiskSize != nil {
			fmt.Printf("    Virtual Disk Size: %d bytes\n", entry.VirtualDiskSize.Size)
		}
		if entry.LogicalSectorSize != nil {
			fmt.Printf("    Logical Sector Size: %d\n", entry.LogicalSectorSize.Size)
		}
		if entry.PhysicalSectorSize != nil {
			fmt.Printf("    Physical Sector Size: %d\n", entry.PhysicalSectorSize.Size)
		}
		if entry.VirtualDiskId != nil {
			fmt.Printf("    Virtual Disk ID: %s\n", utils.StringifyGUID(entry.VirtualDiskId.Id[:]))
		}
		if entry.ParentLocator != nil {
			fmt.Printf("    Parent Locator: keyValueCount=%d\n", entry.ParentLocator.Header.KeyValueCount)
		}
	}

}
