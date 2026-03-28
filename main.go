package main

import (
	"flag"
	"log"
	"vhdxreader/vhdx"
)

func main() {
	evidencePath := flag.String("evidence", "", "Path to the evidence file (VHDX)")
	//offset := flag.Int64("offset", 0, "offset to read data from the evidence")
	//length := flag.Int64("len", 0, "number of bytes to read from offset in the evidence")
	flag.Parse()

	if *evidencePath == "" {
		log.Fatal("Please provide the path to the evidence file using the -evidence flag.")
	}

	vhdx := new(vhdx.Image)
	err := vhdx.ParseEvidence(*evidencePath)
	if err != nil {
		log.Fatalf("Failed to parse evidence: %v", err)
	}

	defer vhdx.Close()
}
