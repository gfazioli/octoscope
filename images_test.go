package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
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
		// Decoded in full for the same reason as a PNG: the quality lives
		// in a table near the top, so a file cut short would still read it.
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Errorf("%s does not decode as a JPEG: %v", path, err)
			continue
		}
		q, err := jpegQuality(data)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if q > 85 {
			t.Errorf("%s reads as quality ~%.0f: run it through ImageOptim with lossy compression on, which caps a JPEG at 85", path, q)
		}
	}
}

// TestJPEGQualityEstimate holds jpegQuality to what the repository's two
// JPEGs, both ordinary and both near 77, never exercise: the 85/86 cutoff on
// files image/jpeg writes with libjpeg's own scaling, a table redefined
// before the scan, markers that carry no length, a frame whose component
// names a table other than 0, and a table that arrives between scans.
func TestJPEGQualityEstimate(t *testing.T) {
	encode := func(quality int) []byte {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 16, 16)), &jpeg.Options{Quality: quality}); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	afterSOI := func(data, insert []byte) []byte {
		return append(append(append([]byte{}, data[:2]...), insert...), data[2:]...)
	}
	// The standard table unscaled is quality 50; the sum ignores its order.
	q50 := []byte{0xFF, 0xDB, 0x00, 2 + 1 + 64, 0x00}
	for _, v := range jpegStdLuminance {
		q50 = append(q50, byte(v))
	}
	// Built by hand, for what image/jpeg never writes: a gray frame whose
	// only component names table 1, and a two-component frame whose first
	// scan leaves out the first component, its table defined only after it.
	segment := func(marker byte, body ...byte) []byte {
		return append([]byte{0xFF, marker, byte((len(body) + 2) >> 8), byte(len(body) + 2)}, body...)
	}
	dqt := func(id byte, quality int) []byte {
		scale := 200 - 2*quality
		body := []byte{id}
		for _, v := range jpegStdLuminance {
			body = append(body, byte(min(255, max(1, (v*scale+50)/100))))
		}
		return segment(0xDB, body...)
	}
	file := func(parts ...[]byte) []byte {
		out := []byte{0xFF, 0xD8}
		for _, p := range parts {
			out = append(out, p...)
		}
		return append(out, 0xFF, 0xD9)
	}
	selectsTable1 := file(dqt(0, 50), dqt(1, 94),
		segment(0xC0, 8, 0, 16, 0, 16, 1, 1, 0x11, 1),
		segment(0xDA, 1, 1, 0x00, 0, 63, 0), []byte{0x12, 0x34})
	latchedLate := file(dqt(0, 50),
		segment(0xC0, 8, 0, 16, 0, 16, 2, 1, 0x11, 1, 2, 0x11, 0),
		segment(0xDA, 1, 2, 0x00, 0, 63, 0), []byte{0x12, 0xFF, 0x00, 0x34, 0xFF, 0xD0, 0x56},
		dqt(1, 94),
		segment(0xDA, 1, 1, 0x00, 0, 63, 0), []byte{0x78})
	for _, tc := range []struct {
		name string
		data []byte
		over bool
	}{
		{"quality 50", encode(50), false},
		{"quality 85", encode(85), false},
		{"quality 86", encode(86), true},
		{"quality 94 after an earlier quality-50 table", afterSOI(encode(94), q50), true},
		{"quality 94 after TEM and RST0", afterSOI(encode(94), []byte{0xFF, 0x01, 0xFF, 0xD0}), true},
		{"a frame naming table 1, at quality 94 beside a quality-50 table 0", selectsTable1, true},
		{"quality 94 defined after a scan of another component", latchedLate, true},
	} {
		q, err := jpegQuality(tc.data)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if (q > 85) != tc.over {
			t.Errorf("%s reads as quality %.2f", tc.name, q)
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

// jpegQuality estimates the quality a JPEG was saved at from the table its
// first component is quantised with, inverting libjpeg's own scaling
// (jpeg_quality_scaling) from the table's sum, which ignores its zigzag
// order. On libjpeg's tables it lands within 0.2 of the setting from 50 to
// 95 and never puts 85 above 85 or 86 at or below it. og-image.jpg read
// ~94 as exported and ~77 after ImageOptim.
func jpegQuality(data []byte) (float64, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, errors.New("not a JPEG")
	}
	var tables [4][]int // a later definition of an id replaces it, as in libjpeg
	component := -1     // the frame's first component
	selector := -1      // and the table it names
	for i := 2; i+1 < len(data); {
		if data[i] != 0xFF {
			return 0, errors.New("marker expected before the scan")
		}
		marker := data[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		if marker == 0x01 || marker >= 0xD0 && marker <= 0xD7 { // TEM, RSTn: no length
			i += 2
			continue
		}
		if i+4 > len(data) {
			return 0, errors.New("segment runs past the end of the file")
		}
		end := i + 2 + int(binary.BigEndian.Uint16(data[i+2:i+4]))
		if end < i+4 || end > len(data) {
			return 0, errors.New("segment runs past the end of the file")
		}
		seg := data[i+4 : end]
		switch {
		case marker == 0xDB: // DQT: one table or several
			for j := 0; j < len(seg); {
				precision, id := seg[j]>>4, int(seg[j]&0x0F)
				size := 64 * (1 + int(precision))
				if precision > 1 || id > 3 || j+1+size > len(seg) {
					return 0, errors.New("malformed quantisation table")
				}
				table := make([]int, 64)
				for k := range table {
					if precision == 0 {
						table[k] = int(seg[j+1+k])
					} else {
						table[k] = int(binary.BigEndian.Uint16(seg[j+1+2*k:]))
					}
				}
				tables[id] = table
				j += 1 + size
			}
		case marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC: // SOFn
			// precision, height (2), width (2), component count, then the
			// first component's id, sampling and table selector
			if len(seg) < 9 {
				return 0, errors.New("malformed frame header")
			}
			component, selector = int(seg[6]), int(seg[8])
		case marker == 0xDA: // start of scan
			// libjpeg latches a component's table at the first scan that
			// carries it, so a scan of other components only means going on:
			// past its header and its entropy-coded data, to the next marker
			// that is neither a stuffed 0xFF00 nor a restart.
			if len(seg) < 1 || len(seg) < 1+2*int(seg[0]) {
				return 0, errors.New("malformed scan header")
			}
			carries := false
			for k := 0; k < int(seg[0]); k++ {
				carries = carries || int(seg[1+2*k]) == component
			}
			if !carries {
				j := end
				for j+1 < len(data) && (data[j] != 0xFF || data[j+1] == 0x00 || data[j+1] >= 0xD0 && data[j+1] <= 0xD7) {
					j++
				}
				i = j
				continue
			}
			if selector < 0 || selector > 3 || tables[selector] == nil {
				return 0, errors.New("no quantisation table for the first component")
			}
			sum, std := 0, 0
			for k, v := range tables[selector] {
				sum += v
				std += jpegStdLuminance[k]
			}
			scale := 100 * float64(sum) / float64(std)
			if scale <= 100 {
				return (200 - scale) / 2, nil
			}
			return 5000 / scale, nil
		}
		i = end
	}
	return 0, errors.New("no scan")
}
