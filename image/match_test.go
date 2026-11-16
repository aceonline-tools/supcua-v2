package image

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"strconv"
	"testing"
)

func encodePNG(t *testing.T, img image.Image) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func randomScreen(width, height int, seed int64) *image.RGBA {
	rng := rand.New(rand.NewSource(seed))
	screen := image.NewRGBA(image.Rect(0, 0, width, height))
	for offset := 0; offset < len(screen.Pix); offset += 4 {
		screen.Pix[offset] = byte(rng.Intn(256))
		screen.Pix[offset+1] = byte(rng.Intn(256))
		screen.Pix[offset+2] = byte(rng.Intn(256))
		screen.Pix[offset+3] = 255
	}
	return screen
}

// legacyFind is the previous At().RGBA()-based implementation.
func legacyFind(screen, template image.Image, tolerance int) (int, int, bool) {
	templateBounds := template.Bounds()
	for scanY := 0; scanY <= screen.Bounds().Dy()-templateBounds.Dy(); scanY++ {
		for scanX := 0; scanX <= screen.Bounds().Dx()-templateBounds.Dx(); scanX++ {
			matched := true
		pixels:
			for y := templateBounds.Min.Y; y < templateBounds.Max.Y; y++ {
				for x := templateBounds.Min.X; x < templateBounds.Max.X; x++ {
					sr, sg, sb, _ := screen.At(scanX+x, scanY+y).RGBA()
					tr, tg, tb, _ := template.At(x, y).RGBA()
					if absDifference(int(sr>>8), int(tr>>8)) > tolerance ||
						absDifference(int(sg>>8), int(tg>>8)) > tolerance ||
						absDifference(int(sb>>8), int(tb>>8)) > tolerance {
						matched = false
						break pixels
					}
				}
			}
			if matched {
				return scanX, scanY, true
			}
		}
	}
	return 0, 0, false
}

func TestValidateSearchSize(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		wantErr       bool
	}{
		{"ok", 100, 50, false},
		{"zero width", 0, 50, true},
		{"zero height", 100, 0, true},
		{"negative", -5, 50, true},
		{"negative height", 100, -1, true},
		{"too large", maxCaptureDimension + 1, 10, true},
		{"area overflow", maxCaptureDimension, maxCaptureDimension, true},
	}
	for _, testCase := range cases {
		err := validateSearchSize(testCase.width, testCase.height)
		if (err != nil) != testCase.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", testCase.name, err, testCase.wantErr)
		}
	}
}

func TestFindMatchParityOpaqueAndTransparent(t *testing.T) {
	screen := randomScreen(40, 30, 1)
	crop := func(x, y, w, h int) *image.RGBA {
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		for row := 0; row < h; row++ {
			copy(out.Pix[row*w*4:(row+1)*w*4], screen.Pix[((y+row)*40+x)*4:((y+row)*40+x+w)*4])
		}
		return out
	}

	nrgba := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	rng := rand.New(rand.NewSource(7))
	for offset := 0; offset < len(nrgba.Pix); offset += 4 {
		nrgba.Pix[offset] = byte(rng.Intn(256))
		nrgba.Pix[offset+1] = byte(rng.Intn(256))
		nrgba.Pix[offset+2] = byte(rng.Intn(256))
		nrgba.Pix[offset+3] = byte(rng.Intn(256))
	}
	gray := image.NewGray(image.Rect(0, 0, 3, 3))
	for i := range gray.Pix {
		gray.Pix[i] = byte(rng.Intn(256))
	}
	paletted := image.NewPaletted(image.Rect(0, 0, 3, 3), color.Palette{color.RGBA{1, 2, 3, 255}, color.NRGBA{200, 100, 50, 128}})
	for i := range paletted.Pix {
		paletted.Pix[i] = uint8(i % 2)
	}

	templates := map[string]image.Image{
		"opaque":      crop(12, 9, 6, 5),
		"transparent": nrgba,
		"gray":        gray,
		"paletted":    paletted,
	}
	for name, template := range templates {
		for _, tolerance := range []int{0, 10, 120} {
			wantX, wantY, wantFound := legacyFind(screen, template, tolerance)
			raw := newRawTemplate(template)
			gotX, gotY, gotFound := findMatch(screen.Pix, 40, 30, raw, tolerance)
			if gotX != wantX || gotY != wantY || gotFound != wantFound {
				t.Errorf("%s tol=%d: got (%d,%d,%v) want (%d,%d,%v)", name, tolerance, gotX, gotY, gotFound, wantX, wantY, wantFound)
			}
		}
	}
	if x, y, found := findMatch(screen.Pix, 40, 30, newRawTemplate(templates["opaque"]), 0); !found || x != 12 || y != 9 {
		t.Errorf("opaque exact match: got (%d,%d,%v)", x, y, found)
	}
}

func TestTemplateCacheHitAndBounds(t *testing.T) {
	cache := newTemplateCache()
	first := encodePNG(t, randomScreen(4, 4, 1))

	a, err := cache.get(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cache.get(first)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || cache.decodes != 1 {
		t.Fatalf("expected cache hit, decodes=%d", cache.decodes)
	}

	for i := 0; i < templateCacheMaxEntries*3; i++ {
		if _, err := cache.get(encodePNG(t, randomScreen(4, 4, int64(100+i)))); err != nil {
			t.Fatal(err)
		}
	}
	if cache.order.Len() > templateCacheMaxEntries || len(cache.entries) > templateCacheMaxEntries {
		t.Fatalf("cache unbounded: %d/%d", cache.order.Len(), len(cache.entries))
	}
	if cache.totalBytes > templateCacheMaxBytes {
		t.Fatalf("cache bytes %d over cap", cache.totalBytes)
	}

	before := cache.decodes
	if _, err := cache.get(first); err != nil {
		t.Fatal(err)
	}
	if cache.decodes != before+1 {
		t.Fatal("evicted entry should be re-decoded")
	}
	if _, err := cache.get("not-base64!!"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestBGRAToRGBAAndBufferReuse(t *testing.T) {
	pix := []byte{1, 2, 3, 0, 10, 20, 30, 0}
	bgraToRGBA(pix)
	want := []byte{3, 2, 1, 255, 30, 20, 10, 255}
	if !bytes.Equal(pix, want) {
		t.Fatalf("got %v want %v", pix, want)
	}
	buffer := acquireCaptureBuffer(64)
	if len(buffer) != 64 {
		t.Fatal("size " + strconv.Itoa(len(buffer)))
	}
	releaseCaptureBuffer(buffer)
	if again := acquireCaptureBuffer(32); len(again) != 32 {
		t.Fatal("reuse size")
	}
}

func TestTemplateLargerThanRegionIsNotFound(t *testing.T) {
	screen := randomScreen(10, 10, 3)
	if err := validateSearchSize(10, 10); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{11, 5}, {5, 11}, {20, 20}} {
		template := newRawTemplate(randomScreen(size[0], size[1], 4))
		if _, _, found := findMatch(screen.Pix, 10, 10, template, 0); found {
			t.Errorf("%v: unexpected match", size)
		}
	}
}
