package main

import (
	"flag"
	"log"
	"vhdxreader/vhdx"
)

func main() {
	evidencePath := flag.String("evidence", "", "Path to the evidence file (VHDX)")
	offset := flag.Int64("offset", 0, "offset to read data from the evidence")
	length := flag.Int64("len", 0, "number of bytes to read from offset in the evidence")
	out := flag.String("out", "", "filename to write raw data")

	flag.Parse()

	if *evidencePath == "" {
		log.Fatal("Please provide the path to the evidence file using the -evidence flag.")
	}

	vhdx := new(vhdx.Image)
	err := vhdx.ParseEvidence(*evidencePath)
	if err != nil {
		log.Fatalf("Failed to parse evidence: %v", err)
	}

	if *out == "" && *offset >= 0 && *length > 0 {
		data, err := vhdx.RetrieveData(*offset, *length)
		if err != nil {
			log.Fatalf("Failed to read data: %v", err)
		}
		log.Printf("Read %d bytes from offset %d", len(data), *offset)
	} else if *out != "" && *offset >= 0 && *length > 0 {
		vhdx.WriteRawFile(*out, *offset, *length)
		log.Printf("Written %d bytes from offset %d to file %s", *length, *offset, *out)
	} else {
		log.Fatal("Please provide valid offset and length using -offset and -len flags, and optionally an output filename using -out flag.")
	}

	defer vhdx.Close()
}
