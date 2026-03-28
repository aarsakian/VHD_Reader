package vhdx

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"
	"vhdxreader/vhdx/header"
	"vhdxreader/vhdx/regions"

	"github.com/aarsakian/FileSystemForensics/utils"
)

// All multi-byte integer fields are little-endian when reading/writing.

// ---------------------------------------------------------------------
// File identifier (File Header) – MS-VHDX 2.2.1
// ---------------------------------------------------------------------

// ---------------------------------------------------------------------
// Convenience container for your parser
// ---------------------------------------------------------------------

type Image struct {
	EvidencePath string
	Handler      *os.File

	FileIdentifier *FileIdentifier
	Header1        *header.VHDXHeader
	Header2        *header.VHDXHeader

	ActiveHeader *header.VHDXHeader

	RegionHeader regions.RegionTableHeader
	Regions      regions.Regions

	Metadata regions.Metadata

	// Decoded metadata

	VirtualSize    uint64
	LogicalSector  uint32
	PhysicalSector uint32
	BlockSize      uint32

	CreatedAt time.Time // if you later decode timestamps from metadata/log
}

func (img *Image) ParseEvidence(path string) error {
	img.EvidencePath = path
	if err := img.CreateHandler(); err != nil {
		return err
	}
	defer img.Close()

	buf := make([]byte, 64*1024)
	if err := img.readAtExact(buf, 0); err != nil {
		return err
	}

	fileIdentifier := new(FileIdentifier)
	_, err := utils.Unmarshal(buf, fileIdentifier)
	if err != nil {
		return err
	}
	img.FileIdentifier = fileIdentifier

	if err := img.readAtExact(buf, 64*1024); err != nil { // read header 1
		return err
	}
	header1 := new(header.VHDXHeader)
	_, err = utils.Unmarshal(buf, header1)
	if err != nil {
		return err
	}
	h1IsValid := header1.IsValid(buf[:4096])
	img.Header1 = header1

	if err := img.readAtExact(buf, 2*64*1024); err != nil {
		return err
	}
	header2 := new(header.VHDXHeader)
	_, err = utils.Unmarshal(buf, header2)
	if err != nil {
		return err
	}
	img.Header2 = header2
	h2IsValid := header2.IsValid(buf[:4096])

	if !h1IsValid && !h2IsValid {
		return errors.New("no valid header found")
	}
	img.DetermineActiveHeader()

	if err := img.readAtExact(buf, 3*64*1024); err != nil {
		return err
	}
	regionHeader1 := new(regions.RegionTableHeader)
	_, err = utils.Unmarshal(buf, regionHeader1)
	if err != nil {
		return err
	}

	regionHeader1IsValid := regionHeader1.IsValid(buf)

	if err := img.readAtExact(buf, 4*64*1024); err != nil {
		return err
	}
	regionHeader2 := new(regions.RegionTableHeader)
	_, err = utils.Unmarshal(buf, regionHeader2)
	if err != nil {
		return err
	}

	regionHeader2IsValid := regionHeader2.IsValid(buf)

	if !regionHeader1IsValid && !regionHeader2IsValid {
		return errors.New("no valid region header found")
	}
	var regionTableOffset int64
	if regionHeader1IsValid {
		img.RegionHeader = *regionHeader1
		regionTableOffset = 3 * 64 * 1024
	} else if regionHeader2IsValid {
		img.RegionHeader = *regionHeader2
		regionTableOffset = 4 * 64 * 1024
	}

	if err := img.readAtExact(buf, regionTableOffset); err != nil {
		return err
	}

	if err := img.ParseRegions(buf[16 : 16+img.RegionHeader.EntryCount*32]); err != nil { // each entry is 32 bytes
		return err
	}
	img.Regions.ShowInfo()

	metadataRegion, err := img.findRegionByGUID(regions.RegionGuidMetadata)
	if err != nil {
		return err
	}

	batRegion, err := img.findRegionByGUID(regions.RegionGuidBAT)
	if err != nil {
		return err
	}

	mt := new(regions.Metadata)
	if err = mt.Parse(img.Handler, metadataRegion.FileOffset); err != nil {
		return err
	}
	mt.ShowInfo()

	img.AddDiskParameters(mt)

	chunkRatio, payLoadBlocksCnt, sectorBitmapBlocksCnt, totalBATEntries :=
		img.DetermineBATLayout()

	fmt.Printf("Chunk Ratio: %d\n", chunkRatio)
	fmt.Printf("Payload Blocks Count: %d\n", payLoadBlocksCnt)
	fmt.Printf("Sector Bitmap Blocks Count: %d\n", sectorBitmapBlocksCnt)
	fmt.Printf("Total BAT Entries: %d\n", totalBATEntries)

	buf = make([]byte, batRegion.Length)
	if err := img.readAtExact(buf, int64(batRegion.FileOffset)); err != nil {
		return err
	}
	bat := new(regions.BAT)
	if err = bat.Parse(buf); err != nil {
		return err
	}

	return nil
}

func (img Image) readAtExact(dst []byte, off int64) error {
	n, err := img.Handler.ReadAt(dst, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n != len(dst) {
		return fmt.Errorf("short read at offset %d: got %d bytes, expected %d", off, n, len(dst))
	}
	return nil
}

func (img Image) findRegionByGUID(guid [16]byte) (*regions.RegionTableEntry, error) {
	for i := range img.Regions {
		if img.Regions[i].Guid == guid {
			return &img.Regions[i], nil
		}
	}
	return nil, fmt.Errorf("required region not found: %s", utils.StringifyGUID(guid[:]))
}

func (img *Image) AddDiskParameters(mt *regions.Metadata) {
	for _, entry := range mt.Entries {
		if entry.FileParameters != nil {
			img.BlockSize = uint32(entry.FileParameters.BlockSize)
		} else if entry.LogicalSectorSize != nil {
			img.LogicalSector = entry.LogicalSectorSize.Size
		} else if entry.PhysicalSectorSize != nil {
			img.PhysicalSector = entry.PhysicalSectorSize.Size
		} else if entry.VirtualDiskSize != nil {
			img.VirtualSize = entry.VirtualDiskSize.Size
		}
	}
}

func (img Image) DetermineBATLayout() (uint32, uint64, uint64, uint64) {
	chunkRatio := 2 ^ 23/img.BlockSize

	payLoadBlocksCnt := math.Ceil(float64(img.VirtualSize) / float64(img.BlockSize))

	sectorBitmapBlocksCnt := math.Ceil(float64(payLoadBlocksCnt) / float64(chunkRatio))

	totalBATEntries := payLoadBlocksCnt + math.Floor(float64(payLoadBlocksCnt)/float64(chunkRatio))
	return chunkRatio, uint64(payLoadBlocksCnt), uint64(sectorBitmapBlocksCnt), uint64(totalBATEntries)
}

func (img *Image) ParseRegions(buf []byte) error {

	img.Regions = make(regions.Regions, img.RegionHeader.EntryCount)

	for i := range img.Regions {
		_, err := utils.Unmarshal(buf[i*32:(i+1)*32], &img.Regions[i])
		if err != nil {
			return err
		}
	}

	return nil
}

func (img *Image) DetermineActiveHeader() {
	if img.Header1.SequenceNumber > img.Header2.SequenceNumber {
		img.ActiveHeader = img.Header1
	} else {
		img.ActiveHeader = img.Header2
	}
}

func (img *Image) CreateHandler() error {
	file, err := os.OpenFile(img.EvidencePath, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	img.Handler = file
	return nil

}

func (img *Image) Close() {
	if img.Handler != nil {
		img.Handler.Close()
		img.Handler = nil
	}

}

type FileIdentifier struct {
	Signature [8]byte // "vhdxfile"
	Creator   [512]byte
}

// ---------------------------------------------------------------------
// Log Entry Header – MS-VHDX 2.2.6 (optional if you implement replay)
// ---------------------------------------------------------------------

type LogEntryHeader struct {
	Signature         [4]byte // "loge"
	Checksum          uint32
	EntryLength       uint32
	Tail              uint32
	SequenceNumber    uint64
	DescriptorCount   uint16
	Reserved          uint16
	FlushedFileOffset uint64
	LastFileOffset    uint64
	Reserved2         [24]byte
}
