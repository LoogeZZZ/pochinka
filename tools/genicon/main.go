// genicon рисует иконки приложения: значок «питания» на цветном круге.
// Запуск из корня проекта: go run ./tools/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

type rgb struct{ r, g, b float64 }

var palettes = map[string][2]rgb{
	"on":  {{0x22, 0xc5, 0x5e}, {0x15, 0x80, 0x3d}},
	"off": {{0x94, 0xa3, 0xb8}, {0x47, 0x55, 0x69}},
	"err": {{0xf8, 0x71, 0x71}, {0xb9, 0x1c, 0x1c}},
	"app": {{0x60, 0xa5, 0xfa}, {0x4f, 0x46, 0xe5}},
}

func main() {
	must(os.MkdirAll("internal/assets", 0o755))
	must(os.MkdirAll("build/windows", 0o755))
	for name, pal := range palettes {
		ico := makeICO(pal, []int{16, 20, 24, 32, 40, 48, 64, 256})
		if name == "app" {
			must(os.WriteFile(filepath.Join("build", "windows", "icon.ico"), ico, 0o644))
			must(os.WriteFile(filepath.Join("internal", "assets", "app.ico"), ico, 0o644))
			var buf bytes.Buffer
			must(png.Encode(&buf, render(1024, pal)))
			must(os.WriteFile(filepath.Join("build", "appicon.png"), buf.Bytes(), 0o644))
			continue
		}
		must(os.WriteFile(filepath.Join("internal", "assets", "tray_"+name+".ico"), ico, 0o644))
	}
}

// render рисует иконку size×size с суперсэмплингом 4×4 для сглаживания.
func render(size int, pal [2]rgb) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	s := float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var cr, cg, cb, ca float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/ss) / s
					py := (float64(y) + (float64(sy)+0.5)/ss) / s
					r, g, b, a := shade(px, py, pal)
					cr += r * a
					cg += g * a
					cb += b * a
					ca += a
				}
			}
			n := float64(ss * ss)
			if ca == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(cr / ca), G: uint8(cg / ca), B: uint8(cb / ca), A: uint8(255 * ca / n),
			})
		}
	}
	return img
}

// shade — цвет точки в координатах 0..1.
func shade(x, y float64, pal [2]rgb) (r, g, b, a float64) {
	dx, dy := x-0.5, y-0.5
	d := math.Hypot(dx, dy)
	if d > 0.48 {
		return 0, 0, 0, 0
	}
	// Диагональный градиент фона.
	t := (x + y) / 2
	r = pal[0].r*(1-t) + pal[1].r*t
	g = pal[0].g*(1-t) + pal[1].g*t
	b = pal[0].b*(1-t) + pal[1].b*t

	// Символ питания: дуга с разрывом сверху + вертикальная палочка.
	const ringR, ringW = 0.22, 0.065
	ang := math.Atan2(dx, -dy) // 0 — строго вверх
	onRing := math.Abs(d-ringR) < ringW/2 && math.Abs(ang) > 0.62
	onBar := math.Abs(dx) < ringW/2 && dy > -0.30 && dy < -0.02
	// Скругления концов палочки.
	if math.Hypot(dx, dy+0.30) < ringW/2 || math.Hypot(dx, dy+0.02) < ringW/2 {
		onBar = true
	}
	if onRing || onBar {
		return 255, 255, 255, 1
	}
	return r, g, b, 1
}

// makeICO собирает ICO с PNG-кадрами (поддерживается Windows начиная с Vista).
func makeICO(pal [2]rgb, sizes []int) []byte {
	var frames [][]byte
	for _, sz := range sizes {
		var buf bytes.Buffer
		must(png.Encode(&buf, render(sz, pal)))
		frames = append(frames, buf.Bytes())
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, sz := range sizes {
		dim := uint8(sz)
		if sz >= 256 {
			dim = 0
		}
		binary.Write(&out, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(frames[i])), uint32(offset)})
		offset += len(frames[i])
	}
	for _, f := range frames {
		out.Write(f)
	}
	return out.Bytes()
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
