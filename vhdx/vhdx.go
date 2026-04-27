package vhdx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
	"vhdxreader/vhdx/header"
	"vhdxreader/vhdx/logger"
	"vhdxreader/vhdx/regions"

	"vhdxreader/utils"
)

var RAW_CHUNK_SIZE int64 = 256 * 1024 * 1024

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
	BatLoc         *BATLocator
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
	img.BatLoc = &BATLocator{ChunkRatio: int((int64(1) << 23) * int64(img.LogicalSector) / int64(img.BlockSize))}
	return nil
}

/*if entry.IsSectorBitmap() {
	// Sector-bitmap entries are addressed in logical-sector units; keep the offset in that range.
	bitmapOffset := offsetInBlock % int64(img.LogicalSector)
	blockBytesToRead = min(int64(img.LogicalSector)-bitmapOffset, remaining)
} else {
	blockBytesToRead = min(int64(img.BlockSize)-offsetInBlock, remaining)
}

if blockBytesToRead <= 0 {
	return nil, fmt.Errorf("invalid read size (%d) at BAT index %d (blockOffset=%d, remaining=%d)", blockBytesToRead, blockStartIndex, blockOffset, remaining)
}
*/

func (img Image) Locate(entryIndex int64) (int64, int64) {
	return img.BatLoc.Locate(entryIndex)
}

func (img Image) RetrieveData(offset, length int64) ([]byte, error) {
	buf := make([]byte, length)

	if offset+length > int64(img.VirtualSize) {
		return nil, fmt.Errorf("requested data exceeds virtual disk size")
	}

	remaining := length
	bufferOffset := int64(0)

	for remaining > 0 {
		entryIndex := offset / int64(img.BlockSize)
		offsetInBlock := offset % int64(img.BlockSize)
		payloadBATIndex, _ := img.Locate(entryIndex)
		entry := img.BAT.Entries[payloadBATIndex]
		dataToRead := min(int64(len(buf)-int(bufferOffset)), int64(img.BlockSize)-offsetInBlock)
		logger.VHDX_Readerlogger.Info(fmt.Sprintf("BAT entry Id %d state=%s, points data at offset %d", entryIndex, entry.GetState(), entry.GetFileOffset()*1024*1024))
		logger.VHDX_Readerlogger.Info(fmt.Sprintf("%s  logical offset %d block offset %d buffer offset %d remaining at block %d data to read %d",
			img.EvidencePath, offset, offsetInBlock, bufferOffset, img.BlockSize-uint32(offsetInBlock), dataToRead))

		if entry.GetState() == "Not Present" && img.HasParent() {
			// Leave the destination range untouched; make already zero-initialized it.

			parentData, err := img.ParentImage.RetrieveData(offset, int64(img.BlockSize)-offsetInBlock)
			if err != nil {
				return nil, fmt.Errorf("failed to retrieve data from parent image: %v", err)
			}

			copy(buf[bufferOffset:], parentData)

		} else if entry.GetState() == "Fully Present" {

			if err := img.readBlockData(entry, offsetInBlock, buf[bufferOffset:bufferOffset+dataToRead]); err != nil {
				return nil, err
			}

		} else if entry.GetState() == "Partially Present" {
			// 0 since I need to check sectorbitMap

			if err := img.RetrieveDataFromSector(entry, offsetInBlock, offset, buf[bufferOffset:bufferOffset+dataToRead]); err != nil {
				return nil, err
			}

		}
		offset += dataToRead
		remaining -= dataToRead

		bufferOffset += dataToRead

	}

	return buf, nil
}

func (img Image) RetrieveDataFromSector(entry regions.BATEntry, offsetInBlock int64, offset int64, buf []byte) error {

	payLoadBATIndex, sectorBitmapBATIndex := img.Locate(offset / int64(img.BlockSize))

	sbEntry := img.BAT.Entries[sectorBitmapBATIndex]

	sectorBitMap := make([]byte, int64(img.BatLoc.ChunkRatio)*int64(img.BlockSize)/int64(img.LogicalSector*8))

	//read data for sector BitMap from the sector bitmap BAT entry
	err := img.readBlockData(sbEntry, 0, sectorBitMap)
	sectorBitMapArray := make([]uint, 8*len(sectorBitMap))
	if err != nil {
		return err
	}

	for i, byteVal := range sectorBitMap {
		for bit := 0; bit < 8; bit++ {
			sectorBitMapArray[8*i+bit] = (uint(byteVal) >> uint(bit)) & 1
		}

	}
	// each sb covers
	sectorIndexStart := payLoadBATIndex*int64(img.LogicalSector)*8 + offsetInBlock/int64(img.LogicalSector)

	sectorCount := int64(len(buf)) / int64(img.LogicalSector)
	sectorIndexEnd := min(sectorIndexStart+sectorCount, int64(len(sectorBitMapArray)))
	bufferOffset := 0

	for i := sectorIndexStart; i < sectorIndexEnd; i++ {
		sectorBytesToRead := min(int64(img.LogicalSector), int64(len(buf))-int64(bufferOffset))

		if sectorBitMapArray[i] == 0 && img.IsDifferencing() { //child block does not have data for this sector, try to read from parent
			logger.VHDX_Readerlogger.Info(fmt.Sprintf("(Sector) %d Retrieving offset %d  len %d buf offset %d ",
				i, offset, sectorBytesToRead, bufferOffset))
			parentData, err := img.ParentImage.RetrieveData(offset, sectorBytesToRead)
			if err != nil {
				return fmt.Errorf("failed to retrieve data from parent image: %v", err)
			}

			copy(buf[bufferOffset:], parentData)

		} else if sectorBitMapArray[i] == 1 && img.IsDifferencing() { //child block has data for this sector

			if err := img.readBlockData(entry, offsetInBlock, buf[bufferOffset:bufferOffset+int(sectorBytesToRead)]); err != nil {
				return err
			}
		}
		offset += sectorBytesToRead
		bufferOffset += int(sectorBytesToRead)

		offsetInBlock += sectorBytesToRead

	}

	return nil
}

func (img Image) IsDifferencing() bool {
	return img.Metadata.HasParent() && img.BAT != nil && len(img.BAT.Entries) > 0
}

func (img Image) IsDynamic() bool {
	return !img.Metadata.HasParent() && img.BAT == nil
}
func (img Image) IsFixed() bool {
	return !img.IsDifferencing() && !img.IsDynamic()
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
	logger.VHDX_Readerlogger.Info(fmt.Sprintf("Reading file %s at offset %d bytes %d",
		img.EvidencePath, blockFileOffset, len(dst)))
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
	chunkRatio := uint32((uint64(1) << 23) * uint64(img.LogicalSector) / uint64(img.BlockSize))

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

func (img Image) WriteRawFile(outfile string, offset int64, length int64) {
	diskSize := img.VirtualSize
	if length > int64(diskSize)-offset {
		panic("Length cannot exceed disk size")
	}

	if length == 0 {
		length = int64(diskSize) - offset
	}

	fmt.Printf("about to write %d MB raw data to %s\n", length/1024/1024, outfile)
	//_buf := BufferPool.Get().([]byte)
	os.Truncate(outfile, 0)
	f, _ := os.OpenFile(outfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()

	var buf bytes.Buffer
	buf.Grow(int(RAW_CHUNK_SIZE))

	data, err := img.RetrieveData(offset, length)
	if err != nil {
		panic(err)
	}
	buf.Write(data)

	f.Write(data)
	buf.Reset()

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

type BATLocator struct {
	ChunkRatio int // number of payload blocks per chunk
}

// Returns the BAT index of the payload block for a given blockIndex.
func (loc BATLocator) PayloadBATIndex(blockIndex int64) int64 {
	chunkSize := int64(loc.ChunkRatio + 1) // PB...PB + SB
	chunkIndex := blockIndex / int64(loc.ChunkRatio)
	offsetInChunk := blockIndex % int64(loc.ChunkRatio)
	return chunkIndex*chunkSize + offsetInChunk
}

// Returns the BAT index of the sector bitmap block for the chunk containing blockIndex.
func (loc BATLocator) SectorBitmapBATIndex(blockIndex int64) int64 {
	chunkSize := int64(loc.ChunkRatio + 1)
	chunkIndex := blockIndex / int64(loc.ChunkRatio)
	return chunkIndex*chunkSize + int64(loc.ChunkRatio) // SB is always last in chunk
}

// Returns both indices at once.
func (loc BATLocator) Locate(blockIndex int64) (payloadBAT, bitmapBAT int64) {
	return loc.PayloadBATIndex(blockIndex), loc.SectorBitmapBATIndex(blockIndex)
}
