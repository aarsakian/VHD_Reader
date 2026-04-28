package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/aarsakian/VHDXReader/vhdx"
	"github.com/aarsakian/VHDXReader/vhdx/logger"
)

func main() {
	evidencePath := flag.String("evidence", "", "Path to the evidence file (VHDX)")
	offset := flag.Int64("offset", 0, "offset to read data from the evidence")
	length := flag.Int64("len", 0, "number of bytes to read from offset in the evidence")
	out := flag.String("out", "", "filename to write raw data")
	logactive := flag.Bool("log", false, "enable logging")
	showinfo := flag.Bool("showinfo", false, "show information about image")

	flag.Parse()

	if *logactive {
		now := time.Now()
		logfilename := "logs" + now.Format("2006-01-02T15_04_05") + ".txt"
		logger.InitializeLogger(*logactive, logfilename)

	}

	if *evidencePath == "" {
		log.Fatal("Please provide the path to the evidence file using the -evidence flag.")
	}

	vhdx := new(vhdx.Image)
	err := vhdx.ParseEvidence(*evidencePath)
	if err != nil {
		log.Fatalf("Failed to parse evidence: %v", err)
	}

	if *showinfo {
		//	vhdx.ShowInfo()
	}

	if *out == "" && *offset >= 0 && *length > 0 {
		logger.VHDX_Readerlogger.Info(fmt.Sprintf("Going to retrieve %d bytes from offset %d in file %s",
			*length, *offset, *evidencePath))
		data, err := vhdx.RetrieveData(*offset, *length)
		if err != nil {
			log.Fatalf("Failed to read data: %v", err)
		}
		log.Printf("Read %d bytes from offset %d", len(data), *offset)
	} else if *out != "" && *offset >= 0 && *length > 0 {
		logger.VHDX_Readerlogger.Info(fmt.Sprintf("Going to retrieve %d bytes from offset %d in file %s and write to %s",
			*length, *offset, *evidencePath, *out))
		vhdx.WriteRawFile(*out, *offset, *length)
		log.Printf("Written %d bytes from offset %d to file %s", *length, *offset, *out)
	} else {
		log.Fatal("Please provide valid offset and length using -offset and -len flags, and optionally an output filename using -out flag.")
	}

	defer vhdx.Close()
}
