package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"reflect"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

var CrC32Table = crc32.MakeTable(crc32.Castagnoli)

func ReadEndianB(barray []byte) (val interface{}) {
	//conversion function
	//fmt.Println("before conversion----------------",barray)
	//fmt.Printf("len%d ",len(barray))

	switch len(barray) {
	case 8:
		var vale uint64
		binary.Read(bytes.NewBuffer(barray), binary.BigEndian, &vale)
		val = vale

	case 4:
		var vale uint32
		//   fmt.Println("barray",barray)
		binary.Read(bytes.NewBuffer(barray), binary.BigEndian, &vale)
		val = vale
		val = vale
	case 2:

		var vale uint16

		binary.Read(bytes.NewBuffer(barray), binary.BigEndian, &vale)
		//   fmt.Println("after conversion vale----------------",barray,vale)
		val = vale

	case 1:

		var vale uint8

		binary.Read(bytes.NewBuffer(barray), binary.BigEndian, &vale)
		//      fmt.Println("after conversion vale----------------",barray,vale)
		val = vale

	default: //best it would be nil
		var vale uint64

		binary.Read(bytes.NewBuffer(barray), binary.BigEndian, &vale)
		val = vale
	}
	return val
}

func Unmarshal(data []byte, v interface{}) (int, error) {
	idx := 0
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem() // get the value pointed to
	}
	if val.Kind() != reflect.Struct {
		return idx, errors.New("requires struct")
	}

	var activeBitBuf uint64
	var bitsRemaining uint

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i) //StructField type
		//	name := val.Type().Field(i).Name
		if idx > len(data) {
			return idx, errors.New("exhausted buffer")
		}

		// Handle bit-field tags: `bits:"N"` extracts N bits from a packed little-endian uint64.
		if bitsTag, hasBits := val.Type().Field(i).Tag.Lookup("bits"); hasBits {
			nBits, err := strconv.Atoi(bitsTag)
			if err != nil || nBits <= 0 || nBits > 64 {
				return idx, fmt.Errorf("invalid bits tag: %s", bitsTag)
			}
			if bitsRemaining == 0 {
				if idx+8 > len(data) {
					return idx, errors.New("exhausted buffer for bit field")
				}
				activeBitBuf = binary.LittleEndian.Uint64(data[idx:])
				idx += 8
				bitsRemaining = 64
			}
			var mask uint64
			if nBits == 64 {
				mask = ^uint64(0)
			} else {
				mask = (uint64(1) << nBits) - 1
			}
			field.SetUint(activeBitBuf & mask)
			activeBitBuf >>= uint(nBits)
			bitsRemaining -= uint(nBits)
			continue
		}

		switch field.Kind() {

		case reflect.Struct:

		case reflect.Pointer:

		case reflect.Uint8:
			if idx+1 > len(data) {
				return idx, errors.New("exhausted buffer")
			}
			field.SetUint(uint64(data[idx]))
			idx += 1
		case reflect.Int16:
			if idx+2 > len(data) {
				return idx, errors.New("exhausted buffer")
			}
			field.SetInt(int64(binary.LittleEndian.Uint16(data[idx:])))
			idx += 2
		case reflect.Uint16:
			if idx+2 > len(data) {
				return idx, errors.New("exhausted buffer")
			}
			field.SetUint(uint64(binary.LittleEndian.Uint16(data[idx:])))
			idx += 2
		case reflect.Uint32:
			if idx+4 > len(data) {
				return idx, errors.New("exhausted buffer")
			}
			field.SetUint(uint64(binary.LittleEndian.Uint32(data[idx:])))
			idx += 4
		case reflect.Int64:
			if idx+8 > len(data) {
				return idx, errors.New("exhausted buffer")
			}
			field.SetInt(int64(binary.LittleEndian.Uint64(data[idx:])))
			idx += 8
		case reflect.Uint64:
			var temp uint64

			if idx+8 > len(data) {
				return idx, errors.New("exceeded available buffer")
			}
			temp = binary.LittleEndian.Uint64(data[idx:])
			idx += 8

			field.SetUint(temp)

		case reflect.Array:
			arrT := reflect.ArrayOf(field.Len(), reflect.TypeOf(data[0])) //create array type to hold the slice
			arr := reflect.New(arrT).Elem()                               //initialize and access array
			var end int
			if idx+field.Len() > len(data) { //determine end
				end = len(data)
			} else {
				end = idx + field.Len()
			}
			if idx >= end {
				return idx, errors.New("exceeded available buffer")
			}
			for idx, val := range data[idx:end] {

				arr.Index(idx).Set(reflect.ValueOf(val))
			}

			field.Set(arr)
			idx += field.Len()

		}

	}
	return idx, nil
}

func StringifyGUID(barray []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", Bytereverse(barray[0:4]),
		Bytereverse(barray[4:6]), Bytereverse(barray[6:8]),
		barray[8:10], barray[10:])
}

func Bytereverse(barray []byte) []byte { //work with indexes
	//  fmt.Println("before",barray)
	for i, j := 0, len(barray)-1; i < j; i, j = i+1, j-1 {

		barray[i], barray[j] = barray[j], barray[i]

	}
	return barray

}

func DecodeUTF16(b []byte) string {
	utf := make([]uint16, (len(b)+(2-1))/2) //2 bytes for one char?
	for i := 0; i+(2-1) < len(b); i += 2 {
		utf[i/2] = binary.LittleEndian.Uint16(b[i:])
	}
	if len(b)/2 < len(utf) {
		utf[len(utf)-1] = utf8.RuneError
	}
	return string(utf16.Decode(utf))

}
