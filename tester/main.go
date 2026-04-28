package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/aarsakian/VHD_Reader/logger"
	"github.com/aarsakian/VHD_Reader/vhdx"
)

// ReadFromPhysicalDrive reads data directly from a physical drive
// driveNumber: the physical drive number (0 for PhysicalDrive0, etc.)
// offset: byte offset to start reading from
// length: number of bytes to read
func ReadFromPhysicalDrive(driveNumber int, offset, length int64) ([]byte, error) {
	drivePath := fmt.Sprintf("\\\\.\\PhysicalDrive%d", driveNumber)

	file, err := os.Open(drivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open physical drive %d: %v", driveNumber, err)
	}
	defer file.Close()

	// Seek to the offset
	_, err = file.Seek(offset, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to offset %d on physical drive %d: %v", offset, driveNumber, err)
	}

	// Read the data
	buf := make([]byte, length)
	bytesRead, err := file.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read from physical drive %d at offset %d: %v", driveNumber, offset, err)
	}

	if int64(bytesRead) != length {
		log.Printf("Warning: requested %d bytes but only read %d bytes from physical drive %d at offset %d\n", length, bytesRead, driveNumber, offset)
	}

	return buf[:bytesRead], nil
}

func main() {
	// Parse command-line arguments
	// Usage: tester.exe <evidence_file_path> <physical_drive_number> [offset] [length]

	if len(os.Args) < 3 {
		fmt.Println("Usage: tester.exe <evidence_file_path> <physical_drive_number> [offset] [length]")
		fmt.Println("  evidence_file_path: Path to the VHDX evidence file")
		fmt.Println("  physical_drive_number: Physical drive number to compare (e.g., 0 for PhysicalDrive0)")
		fmt.Println("  offset: Optional byte offset to read from (default: 0)")
		fmt.Println("  length: Optional number of bytes to read (default: 4096)")
		os.Exit(1)
	}

	evidencePath := os.Args[1]
	driveNumber, err := strconv.Atoi(os.Args[2])
	if err != nil {
		log.Fatalf("Invalid physical drive number: %v", err)
	}

	// Parse optional offset and length
	offset := int64(0)
	length := int64(4096)

	if len(os.Args) > 3 {
		val, err := strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			log.Fatalf("Invalid offset: %v", err)
		}
		offset = val
	}

	if len(os.Args) > 4 {
		val, err := strconv.ParseInt(os.Args[4], 10, 64)
		if err != nil {
			log.Fatalf("Invalid length: %v", err)
		}
		length = val
	}

	log.Printf("Testing RetrieveData function")
	log.Printf("Evidence file: %s", evidencePath)
	log.Printf("Physical drive: %d", driveNumber)
	log.Printf("Offset: %d, Length: %d bytes", offset, length)
	log.Println()

	// Initialize logger
	logger.InitializeLogger(true, "tester_logs.txt")

	// Parse the evidence file
	log.Println("Parsing evidence file...")
	vhdxImage := new(vhdx.Image)
	err = vhdxImage.ParseEvidence(evidencePath)
	if err != nil {
		log.Fatalf("Failed to parse evidence: %v", err)
	}
	defer vhdxImage.Close()

	log.Printf("Successfully parsed evidence file")
	log.Printf("Virtual size: %d bytes", vhdxImage.VirtualSize)
	log.Printf("Logical size: %d bytes", vhdxImage.LogicalSize)
	log.Println()

	// Validate offset and length
	if offset+length > int64(vhdxImage.VirtualSize) {
		log.Fatalf("Requested data exceeds virtual disk size. Offset: %d, Length: %d, Virtual size: %d",
			offset, length, vhdxImage.VirtualSize)
	}

	// Retrieve data from VHDX
	log.Println("Retrieving data from VHDX using RetrieveData()...")
	vhdxData, err := vhdxImage.RetrieveData(offset, length)
	if err != nil {
		log.Fatalf("Failed to retrieve data from VHDX: %v", err)
	}
	log.Printf("Successfully retrieved %d bytes from VHDX", len(vhdxData))
	log.Println()

	// Retrieve data from physical drive
	log.Printf("Retrieving data from physical drive %d...", driveNumber)
	driveData, err := ReadFromPhysicalDrive(driveNumber, offset, length)
	if err != nil {
		log.Fatalf("Failed to retrieve data from physical drive: %v", err)
	}
	log.Printf("Successfully retrieved %d bytes from physical drive", len(driveData))
	log.Println()

	// Compare the data
	log.Println("Comparing data...")
	if len(vhdxData) != len(driveData) {
		log.Printf("ERROR: Data lengths differ! VHDX: %d bytes, Physical drive: %d bytes",
			len(vhdxData), len(driveData))
	} else if compareBytes(vhdxData, driveData) {
		log.Printf("SUCCESS: Data from VHDX matches data from physical drive!")
		log.Printf("Both contain %d bytes of identical data at offset %d", len(vhdxData), offset)
	} else {
		log.Printf("ERROR: Data mismatch! VHDX and physical drive data differ at offset %d", offset)
		// Show first difference
		for i := 0; i < len(vhdxData); i++ {
			if vhdxData[i] != driveData[i] {
				log.Printf("First difference at byte %d: VHDX=0x%02x, Drive=0x%02x", i, vhdxData[i], driveData[i])
				break
			}
		}
	}
}

// compareBytes compares two byte slices and returns true if they are identical
func compareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
