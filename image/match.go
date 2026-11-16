package image

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"sync"
)

const (
	maxCaptureDimension = 32768
	maxCapturePixels    = 1 << 26

	templateCacheMaxEntries = 8
	templateCacheMaxBytes   = 32 << 20
)

// validateSearchSize rejects sizes that cannot be captured. A template larger
// than the region is not an error: findMatch simply finds nothing.
func validateSearchSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid search region size %dx%d", width, height)
	}
	if width > maxCaptureDimension || height > maxCaptureDimension || width*height > maxCapturePixels {
		return fmt.Errorf("search region %dx%d too large", width, height)
	}
	return nil
}

// rawTemplate holds template pixels as 8-bit premultiplied RGBA, identical to
// color.Color.RGBA()>>8 of the decoded image.
type rawTemplate struct {
	width, height int
	pix           []byte
}

func newRawTemplate(decoded image.Image) *rawTemplate {
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	pix := make([]byte, width*height*4)
	offset := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := decoded.At(x, y).RGBA()
			pix[offset+0] = byte(r >> 8)
			pix[offset+1] = byte(g >> 8)
			pix[offset+2] = byte(b >> 8)
			pix[offset+3] = byte(a >> 8)
			offset += 4
		}
	}
	return &rawTemplate{width: width, height: height, pix: pix}
}

func decodeRawTemplate(encoded string) (*rawTemplate, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	decoded, err := png.Decode(bytes.NewReader(decodedBytes))
	if err != nil {
		return nil, err
	}
	return newRawTemplate(decoded), nil
}

type templateCacheEntry struct {
	key      [sha256.Size]byte
	template *rawTemplate
}

// templateCache is a mutex-guarded LRU bounded by entry count and pixel bytes.
// Keys are content hashes, so a changed template can never hit a stale entry.
type templateCache struct {
	mutex      sync.Mutex
	entries    map[[sha256.Size]byte]*list.Element
	order      *list.List
	totalBytes int
	decodes    int
}

func newTemplateCache() *templateCache {
	return &templateCache{entries: map[[sha256.Size]byte]*list.Element{}, order: list.New()}
}

func (cache *templateCache) get(encoded string) (*rawTemplate, error) {
	key := sha256.Sum256([]byte(encoded))

	cache.mutex.Lock()
	if element, ok := cache.entries[key]; ok {
		cache.order.MoveToFront(element)
		template := element.Value.(*templateCacheEntry).template
		cache.mutex.Unlock()
		return template, nil
	}
	cache.mutex.Unlock()

	template, err := decodeRawTemplate(encoded)
	if err != nil {
		return nil, err
	}

	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.decodes++
	if element, ok := cache.entries[key]; ok {
		cache.order.MoveToFront(element)
		return element.Value.(*templateCacheEntry).template, nil
	}
	size := len(template.pix)
	if size > templateCacheMaxBytes {
		return template, nil
	}
	cache.entries[key] = cache.order.PushFront(&templateCacheEntry{key: key, template: template})
	cache.totalBytes += size
	for cache.order.Len() > templateCacheMaxEntries || cache.totalBytes > templateCacheMaxBytes {
		oldest := cache.order.Back()
		evicted := oldest.Value.(*templateCacheEntry)
		cache.order.Remove(oldest)
		delete(cache.entries, evicted.key)
		cache.totalBytes -= len(evicted.template.pix)
	}
	return template, nil
}

var sharedTemplateCache = newTemplateCache()

// findMatch scans an opaque RGBA screen buffer (stride width*4) for the template.
// Only R, G, B are compared, matching the previous At().RGBA()>>8 logic.
func findMatch(screenPix []byte, screenWidth, screenHeight int, template *rawTemplate, tolerance int) (int, int, bool) {
	templateWidth, templateHeight := template.width, template.height
	screenStride := screenWidth * 4
	templateStride := templateWidth * 4
	for scanY := 0; scanY <= screenHeight-templateHeight; scanY++ {
		for scanX := 0; scanX <= screenWidth-templateWidth; scanX++ {
			if matchAt(screenPix, screenStride, scanX, scanY, template.pix, templateStride, templateWidth, templateHeight, tolerance) {
				return scanX, scanY, true
			}
		}
	}
	return 0, 0, false
}

func matchAt(screenPix []byte, screenStride, startX, startY int, templatePix []byte, templateStride, templateWidth, templateHeight, tolerance int) bool {
	for row := 0; row < templateHeight; row++ {
		screenRow := screenPix[(startY+row)*screenStride+startX*4:]
		templateRow := templatePix[row*templateStride : (row+1)*templateStride]
		for column := 0; column < templateStride; column += 4 {
			if absDifference(int(screenRow[column]), int(templateRow[column])) > tolerance ||
				absDifference(int(screenRow[column+1]), int(templateRow[column+1])) > tolerance ||
				absDifference(int(screenRow[column+2]), int(templateRow[column+2])) > tolerance {
				return false
			}
		}
	}
	return true
}

func absDifference(valueA, valueB int) int {
	if valueA > valueB {
		return valueA - valueB
	}
	return valueB - valueA
}

// bgraToRGBA converts in place and forces opaque alpha.
func bgraToRGBA(pix []byte) {
	for offset := 0; offset+3 < len(pix); offset += 4 {
		pix[offset], pix[offset+2] = pix[offset+2], pix[offset]
		pix[offset+3] = 255
	}
}

var capturePool sync.Pool

// acquireCaptureBuffer returns a buffer of exactly size bytes owned solely by
// the caller until releaseCaptureBuffer.
func acquireCaptureBuffer(size int) []byte {
	if pooled, ok := capturePool.Get().(*[]byte); ok && cap(*pooled) >= size {
		return (*pooled)[:size]
	}
	return make([]byte, size)
}

func releaseCaptureBuffer(buffer []byte) {
	capturePool.Put(&buffer)
}
