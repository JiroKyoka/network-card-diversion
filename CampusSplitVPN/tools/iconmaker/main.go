// Command iconmaker converts a square source image into macOS ICNS and
// Windows multi-resolution ICO containers using only the Go standard library.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
)

type iconImage struct {
	size int
	png  []byte
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: iconmaker SOURCE_IMAGE OUTPUT.icns OUTPUT.ico")
		os.Exit(2)
	}
	file, err := os.Open(os.Args[1])
	check(err)
	source, _, err := image.Decode(file)
	_ = file.Close()
	check(err)

	sizes := []int{16, 24, 32, 48, 64, 128, 256, 512, 1024}
	icons := make(map[int]iconImage, len(sizes))
	for _, size := range sizes {
		var data bytes.Buffer
		check(png.Encode(&data, resize(source, size)))
		icons[size] = iconImage{size: size, png: data.Bytes()}
	}
	check(writeICNS(os.Args[2], icons))
	check(writeICO(os.Args[3], icons, []int{16, 24, 32, 48, 64, 128, 256}))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "iconmaker:", err)
		os.Exit(1)
	}
}

func resize(source image.Image, size int) *image.NRGBA {
	bounds := source.Bounds()
	result := image.NewNRGBA(image.Rect(0, 0, size, size))
	sx := float64(bounds.Dx()) / float64(size)
	sy := float64(bounds.Dy()) / float64(size)
	for y := 0; y < size; y++ {
		fy := (float64(y)+0.5)*sy - 0.5
		y0 := int(math.Floor(fy))
		wy := fy - float64(y0)
		if y0 < 0 {
			y0, wy = 0, 0
		}
		y1 := min(y0+1, bounds.Dy()-1)
		for x := 0; x < size; x++ {
			fx := (float64(x)+0.5)*sx - 0.5
			x0 := int(math.Floor(fx))
			wx := fx - float64(x0)
			if x0 < 0 {
				x0, wx = 0, 0
			}
			x1 := min(x0+1, bounds.Dx()-1)
			pixels := [4][4]uint32{}
			pixels[0][0], pixels[0][1], pixels[0][2], pixels[0][3] = source.At(bounds.Min.X+x0, bounds.Min.Y+y0).RGBA()
			pixels[1][0], pixels[1][1], pixels[1][2], pixels[1][3] = source.At(bounds.Min.X+x1, bounds.Min.Y+y0).RGBA()
			pixels[2][0], pixels[2][1], pixels[2][2], pixels[2][3] = source.At(bounds.Min.X+x0, bounds.Min.Y+y1).RGBA()
			pixels[3][0], pixels[3][1], pixels[3][2], pixels[3][3] = source.At(bounds.Min.X+x1, bounds.Min.Y+y1).RGBA()
			for channel := 0; channel < 4; channel++ {
				top := float64(pixels[0][channel])*(1-wx) + float64(pixels[1][channel])*wx
				bottom := float64(pixels[2][channel])*(1-wx) + float64(pixels[3][channel])*wx
				result.Pix[y*result.Stride+x*4+channel] = uint8((top*(1-wy) + bottom*wy) / 257)
			}
		}
	}
	return result
}

func writeICNS(path string, icons map[int]iconImage) error {
	// Modern ICNS chunks contain complete PNG images. Retina chunks reuse the
	// equivalent pixel dimensions so Finder has the full scale matrix.
	chunks := []struct {
		kind string
		size int
	}{
		{"icp4", 16}, {"ic11", 32}, {"icp5", 32}, {"ic12", 64},
		{"ic07", 128}, {"ic13", 256}, {"ic08", 256},
		{"ic14", 512}, {"ic09", 512}, {"ic10", 1024},
	}
	total := 8
	for _, chunk := range chunks {
		total += 8 + len(icons[chunk.size].png)
	}
	var output bytes.Buffer
	output.WriteString("icns")
	_ = binary.Write(&output, binary.BigEndian, uint32(total))
	for _, chunk := range chunks {
		data := icons[chunk.size].png
		output.WriteString(chunk.kind)
		_ = binary.Write(&output, binary.BigEndian, uint32(8+len(data)))
		output.Write(data)
	}
	return os.WriteFile(path, output.Bytes(), 0644)
}

func writeICO(path string, icons map[int]iconImage, sizes []int) error {
	var output bytes.Buffer
	_ = binary.Write(&output, binary.LittleEndian, uint16(0))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for _, size := range sizes {
		dimension := byte(size)
		if size == 256 {
			dimension = 0
		}
		output.WriteByte(dimension)
		output.WriteByte(dimension)
		output.WriteByte(0)
		output.WriteByte(0)
		_ = binary.Write(&output, binary.LittleEndian, uint16(1))
		_ = binary.Write(&output, binary.LittleEndian, uint16(32))
		_ = binary.Write(&output, binary.LittleEndian, uint32(len(icons[size].png)))
		_ = binary.Write(&output, binary.LittleEndian, uint32(offset))
		offset += len(icons[size].png)
	}
	for _, size := range sizes {
		output.Write(icons[size].png)
	}
	return os.WriteFile(path, output.Bytes(), 0644)
}
