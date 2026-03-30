package vhdx

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
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

	ActiveHeader *header.VHDXHeader

	Metadata *regions.Metadata
	BAT      *regions.BAT

	// Decoded metadata
	LogicalSize    uint64
	VirtualSize    uint64
	LogicalSector  uint32
	PhysicalSector uint32
	BlockSize      uint32
	ParentImage    *Image
	CreatedAt      time.Time // if you later decode timestamps from metadata/log
}

type BlockDataBoundsError struct {
	Offset      int64
	Length      int64
	LogicalSize int64
}

func (e *BlockDataBoundsError) Error() string {
	return fmt.Sprintf("read block data out of bounds: offset=%d length=%d logicalSize=%d", e.Offset, e.Length, e.LogicalSize)
}

type BlockDataReadError struct {
	Offset int64
	Length int64
	Err    error
}

func (e *BlockDataReadError) Error() string {
	return fmt.Sprintf("read block data failed: offset=%d length=%d: %v", e.Offset, e.Length, e.Err)
}

func (e *BlockDataReadError) Unwrap() error {
	return e.Err
}

func (img Image) HasParent() bool {
	return img.Metadata.HasParent()
}

func (img *Image) ParseEvidence(path string) (err error) {
	img.EvidencePath = path
	if err := img.CreateHandler(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			img.Close()
		}
	}()

	buf := make([]byte, 64*1024)
	if err := img.readAtExact(buf, 0); err != nil {
		return err
	}

	fileIdentifier := new(FileIdentifier)
	_, err = utils.Unmarshal(buf, fileIdentifier)
	if err != nil {
		return err
	}
	img.FileIdentifier = fileIdentifier

	if err := img.readAtExact(buf, 64*1024); err != nil { // read header 1
		return err
	}
	header1 := new(header.VHDXHeader)
	err = header1.Parse(buf)
	if err != nil {
		return err
	}
	if err := img.readAtExact(buf, 2*64*1024); err != nil {
		return err
	}
	header2 := new(header.VHDXHeader)
	err = header2.Parse(buf)
	if err != nil {
		return err
	}

	if !header1.IsValid(buf[:4096]) && !header2.IsValid(buf[:4096]) {
		return errors.New("no valid header found")
	}
	img.DetermineActiveHeader(header1, header2)

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
	var activeRegionHeader regions.RegionTableHeader
	var regionTableOffset int64
	if regionHeader1IsValid {
		activeRegionHeader = *regionHeader1
		regionTableOffset = 3 * 64 * 1024
	} else if regionHeader2IsValid {
		activeRegionHeader = *regionHeader2
		regionTableOffset = 4 * 64 * 1024
	}

	if err := img.readAtExact(buf, regionTableOffset); err != nil {
		return err
	}

	regionEntries, err := img.ParseRegions(buf[16 : 16+activeRegionHeader.EntryCount*32]) // each entry is 32 bytes
	if err != nil {
		return err
	}
	regionEntries.ShowInfo()

	metadataRegion, err := img.findRegionByGUID(regionEntries, regions.RegionGuidMetadata)
	if err != nil {
		return err
	}

	batRegion, err := img.findRegionByGUID(regionEntries, regions.RegionGuidBAT)
	if err != nil {
		return err
	}

	img.Metadata = new(regions.Metadata)
	if err = img.Metadata.Parse(img.Handler, metadataRegion.FileOffset); err != nil {
		return err
	}
	img.Metadata.ShowInfo()

	if err := img.AddDiskParameters(); err != nil {
		return err
	}

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
	img.BAT = new(regions.BAT)
	if err = img.BAT.Parse(buf, int(chunkRatio)); err != nil {
		return err

	}

	parentImagePath := img.LocateParentImage()
	if parentImagePath != "" {
		basepath := filepath.Dir(img.EvidencePath)
		parentImagePath = filepath.Join(basepath, parentImagePath)
		fmt.Printf("Parent image located at: %s\n", parentImagePath)
		img.ParentImage = new(Image)
		if err := img.ParentImage.ParseEvidence(parentImagePath); err != nil {
			return fmt.Errorf("failed to parse parent image: %v", err)
		}
	}

	return nil
}

func (img Image) RetrieveData(offset, length int64) ([]byte, error) {
	buf := make([]byte, length)
	var bytesToRead int64
	if offset+length > int64(img.VirtualSize) {
		return nil, fmt.Errorf("requested data exceeds virtual disk size")
	}

	blockStartIndex := offset / int64(img.BlockSize)
	blockOffset := offset % int64(img.BlockSize)
	remaining := length
	dstOffset := int64(0)

	for remaining > 0 {
		entry := img.BAT.Entries[blockStartIndex]
		if entry.IsSectorBitmap() {
			bytesToRead = min(int64(img.LogicalSector)-blockOffset, remaining)
		} else {
			bytesToRead = min(int64(img.BlockSize)-blockOffset, remaining)
		}

		if entry.GetState() == "Not Present" || entry.GetState() == "Undefined" {
			// Leave the destination range untouched; make already zero-initialized it.
			if img.HasParent() {
				parentData, err := img.ParentImage.RetrieveData(offset, bytesToRead)
				if err != nil {
					return nil, fmt.Errorf("failed to retrieve data from parent image: %v", err)
				}
				copy(buf[dstOffset:dstOffset+bytesToRead], parentData)
				bytesToRead = int64(len(parentData))
			}
		} else if entry.GetState() == "Fully Present" {
			if err := img.readBlockData(entry, blockOffset, buf[dstOffset:dstOffset+bytesToRead]); err != nil {
				return nil, err
			}
		} else if entry.GetState() == "Partially Present" {

			err := img.readBlockData(entry, blockOffset, buf[dstOffset:dstOffset+bytesToRead])
			if err == nil {
				sectorBitMap := make([]byte, int64(img.BlockSize)/int64(img.LogicalSector)/8)

				pos := 0
				for i := int64(0); i < int64(len(sectorBitMap)); i++ {
					if i > 0 && i%8 == 0 {
						pos++
					}
					sectorBitMap[i] = buf[pos] & (1 << (7 - (i % 8)))

				}
				sectorOffset := int64(0)
				for _, sectorStatus := range sectorBitMap {
					if remaining <= 0 {
						break
					}
					if sectorStatus == 0 {
						parentData, err := img.ParentImage.RetrieveData(sectorOffset, int64(img.LogicalSector))
						bytesToRead = int64(len(parentData))
						if err != nil {
							return nil, fmt.Errorf("failed to retrieve data from parent image: %v", err)
						}
						copy(buf[dstOffset:dstOffset+int64(img.LogicalSector)], parentData)
					} else { //child block has data for this sector

					}
					sectorOffset += int64(img.LogicalSector)

					dstOffset += int64(img.LogicalSector)
					remaining -= int64(img.LogicalSector)

				}
			} else if err.Error() == "ReadBlockDataBoundsError" {
				return nil, err
			}

		} else if entry.GetState() == "Present" {

			if err := img.readBlockData(entry, blockOffset, buf[dstOffset:dstOffset+bytesToRead]); err != nil {
				return nil, err
			}
		}

		remaining -= bytesToRead
		dstOffset += bytesToRead
		blockStartIndex++
		blockOffset = 0
	}

	return buf, nil
}

func (img Image) readBlockData(entry regions.BATEntry, blockOffset int64, dst []byte) error {
	readLength := int64(len(dst))
	if readLength == 0 {
		return nil
	}

	if blockOffset < 0 {
		return &BlockDataBoundsError{Offset: blockOffset, Length: readLength, LogicalSize: int64(img.LogicalSize)}
	}

	// BAT file offsets are expressed in MiB units (MS-VHDX 2.2.5).
	const mib = int64(1024 * 1024)
	entryOffsetMB := entry.GetFileOffset()
	if entryOffsetMB > uint64(math.MaxInt64)/uint64(mib) {
		return &BlockDataBoundsError{Offset: math.MaxInt64, Length: readLength, LogicalSize: int64(img.LogicalSize)}
	}

	baseOffset := int64(entryOffsetMB) * mib
	if blockOffset > math.MaxInt64-baseOffset {
		return &BlockDataBoundsError{Offset: math.MaxInt64, Length: readLength, LogicalSize: int64(img.LogicalSize)}
	}

	blockFileOffset := baseOffset + blockOffset
	logicalSize := int64(img.LogicalSize)
	if blockFileOffset < 0 || blockFileOffset >= logicalSize || blockFileOffset+readLength > logicalSize {
		return &BlockDataBoundsError{
			Offset:      blockFileOffset,
			Length:      readLength,
			LogicalSize: logicalSize,
		}
	}
	if err := img.readAtExact(dst, blockFileOffset); err != nil {
		return &BlockDataReadError{
			Offset: blockFileOffset,
			Length: readLength,
			Err:    err,
		}
	}
	return nil
}

func (img Image) readAtExact(dst []byte, off int64) error {
	if img.Handler == nil {
		return errors.New("read on nil file handler")
	}
	if off < 0 {
		return fmt.Errorf("negative read offset: %d", off)
	}
	n, err := img.Handler.ReadAt(dst, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n != len(dst) {
		return fmt.Errorf("short read at offset %d: got %d bytes, expected %d", off, n, len(dst))
	}
	return nil
}

func (img Image) findRegionByGUID(regionEntries regions.Regions, guid [16]byte) (*regions.RegionTableEntry, error) {
	for i := range regionEntries {
		if regionEntries[i].Guid == guid {
			return &regionEntries[i], nil
		}
	}
	return nil, fmt.Errorf("required region not found: %s", utils.StringifyGUID(guid[:]))
}

func (img Image) LocateParentImage() string {
	for _, entry := range img.Metadata.Entries {
		if entry.ParentLocator != nil {
			for key, locator := range entry.ParentLocator.Locators {
				if key == "relative_path" {
					return locator
				}
			}
		}
	}
	return ""
}

func (img *Image) AddDiskParameters() error {
	info, err := os.Stat(img.EvidencePath)
	if err != nil {
		return err
	}
	img.CreatedAt = info.ModTime()
	img.LogicalSize = uint64(info.Size())

	for _, entry := range img.Metadata.Entries {
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
	return nil
}

func (img Image) DetermineBATLayout() (uint32, uint64, uint64, uint64) {
	chunkRatio := 2 ^ 23/img.BlockSize

	payLoadBlocksCnt := math.Ceil(float64(img.VirtualSize) / float64(img.BlockSize))

	sectorBitmapBlocksCnt := math.Ceil(float64(payLoadBlocksCnt) / float64(chunkRatio))

	totalBATEntries := payLoadBlocksCnt + math.Floor(float64(payLoadBlocksCnt)/float64(chunkRatio))
	return chunkRatio, uint64(payLoadBlocksCnt), uint64(sectorBitmapBlocksCnt), uint64(totalBATEntries)
}

func (img *Image) ParseRegions(buf []byte) (regions.Regions, error) {
	entryCount := len(buf) / 32
	regionEntries := make(regions.Regions, entryCount)

	for i := range regionEntries {
		_, err := utils.Unmarshal(buf[i*32:(i+1)*32], &regionEntries[i])
		if err != nil {
			return nil, err
		}
	}

	return regionEntries, nil
}

func (img *Image) DetermineActiveHeader(header1, header2 *header.VHDXHeader) {
	if header1.SequenceNumber > header2.SequenceNumber {
		img.ActiveHeader = header1
	} else {
		img.ActiveHeader = header2
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
