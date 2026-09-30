package main

import (
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every image the site serves has been through ImageOptim with lossy
// compression on. On 2026-09-30 that took the repository's 28 images from
// 8.86 MB to 3.24 MB, every screenshot at SSIM ≥ 0.998, and Chrome decoded
// the carousel six times faster (223 → 37 ms). An image that skipped the
// step looks like any other in a diff, so these tests read what the step
// leaves behind: a PNG becomes a palette, a JPEG drops to quality 85 or
// under. A still copied from tapes/out/ is neither.

// truecolourByDesign are the PNGs ImageOptim leaves truecolour: the two
// favicons, 511 and 1,179 bytes after it.
var truecolourByDesign = map[string]bool{
	"docs/icons/favicon-16.png": true,
	"docs/icons/favicon-32.png": true,
}

// siteImages lists the files under docs/ with one of exts, as slash paths.
func siteImages(t *testing.T, exts ...string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir("docs", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		for _, ext := range exts {
			if strings.EqualFold(filepath.Ext(path), ext) {
				paths = append(paths, filepath.ToSlash(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("found no %v under docs/: the check would measure nothing", exts)
	}
	return paths
}

func TestSitePNGsAreQuantized(t *testing.T) {
	for _, path := range siteImages(t, ".png") {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		// A full decode rather than a look at the header: it also refuses
		// a truncated or corrupt file, which a colour-type byte would pass.
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Errorf("%s does not decode as a PNG: %v", path, err)
			continue
		}
		if _, ok := img.(*image.Paletted); !ok && !truecolourByDesign[path] {
			t.Errorf("%s is truecolour (%T): run it through ImageOptim with lossy compression on, or pngquant, before committing it", path, img)
		}
	}
}

func TestSiteJPEGsAreRecompressed(t *testing.T) {
	for _, path := range siteImages(t, ".jpg", ".jpeg") {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		q, err := jpegQuality(data)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if q > 85 {
			t.Errorf("%s was saved at quality ~%.0f: run it through ImageOptim with lossy compression on, which caps a JPEG at 85", path, q)
		}
	}
}

// jpegStdLuminance is the luminance quantisation table of the JPEG
// standard (Annex K), the one libjpeg scales by its quality setting.
var jpegStdLuminance = [64]int{
	16, 11, 10, 16, 24, 40, 51, 61,
	12, 12, 14, 19, 26, 58, 60, 55,
	14, 13, 16, 24, 40, 57, 69, 56,
	14, 17, 22, 29, 51, 87, 80, 62,
	18, 22, 37, 56, 68, 109, 103, 77,
	24, 35, 55, 64, 81, 104, 113, 92,
	49, 64, 78, 87, 103, 121, 120, 101,
	72, 92, 95, 98, 112, 100, 103, 99,
}

// jpegQuality estimates the quality a JPEG was saved at from its luminance
// table, inverting libjpeg's scaling the way ImageMagick guesses it. Only
// the sum is compared, so the table's zigzag order does not matter.
// og-image.jpg read ~94 as exported and ~77 after ImageOptim.
func jpegQuality(data []byte) (float64, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, errors.New("not a JPEG")
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 0, errors.New("marker expected before the scan")
		}
		marker := data[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		if marker == 0xDA { // start of scan: no table came first
			break
		}
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		end := i + 2 + length
		if length < 2 || end > len(data) {
			return 0, errors.New("segment runs past the end of the file")
		}
		for j := i + 4; marker == 0xDB && j < end; {
			precision, id := data[j]>>4, data[j]&0x0F
			size := 64 * (1 + int(precision))
			if j+1+size > end {
				return 0, errors.New("quantisation table runs past its segment")
			}
			if id == 0 {
				sum, std := 0, 0
				for k := 0; k < 64; k++ {
					if precision == 0 {
						sum += int(data[j+1+k])
					} else {
						sum += int(binary.BigEndian.Uint16(data[j+1+2*k:]))
					}
					std += jpegStdLuminance[k]
				}
				scale := 100 * float64(sum) / float64(std)
				if scale <= 100 {
					return (200 - scale) / 2, nil
				}
				return 5000 / scale, nil
			}
			j += 1 + size
		}
		i = end
	}
	return 0, errors.New("no luminance quantisation table")
}
