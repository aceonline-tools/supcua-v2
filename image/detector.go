//go:build windows

package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"

	"github.com/yanmad27/supcua-v2/model"
)

var (
	user32DLL                      = syscall.NewLazyDLL("user32.dll")
	gdi32DLL                       = syscall.NewLazyDLL("gdi32.dll")
	procGetDC                      = user32DLL.NewProc("GetDC")
	procReleaseDC                  = user32DLL.NewProc("ReleaseDC")
	procGetSystemMetrics           = user32DLL.NewProc("GetSystemMetrics")
	procCreateCompatibleDC         = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap     = gdi32DLL.NewProc("CreateCompatibleBitmap")
	procSelectObject               = gdi32DLL.NewProc("SelectObject")
	procBitBlt                     = gdi32DLL.NewProc("BitBlt")
	procDeleteObject               = gdi32DLL.NewProc("DeleteObject")
	procDeleteDC                   = gdi32DLL.NewProc("DeleteDC")
	procGetDIBits                  = gdi32DLL.NewProc("GetDIBits")
)

const (
	systemMetricScreenWidth  = 0
	systemMetricScreenHeight = 1
	bitBltSourceCopy         = 0x00CC0020
)

// Detector captures screen regions and matches template images via pixel comparison.
type Detector struct {
	matchTolerance int // per-channel tolerance (0-255)
}

// NewDetector creates an image Detector with the given per-channel tolerance.
func NewDetector(matchTolerance int) *Detector {
	return &Detector{matchTolerance: matchTolerance}
}

// FindImage searches the screen (or a region) for the given base64-encoded PNG.
// Returns the center coordinates of the match and whether it was found.
func (imageDetector *Detector) FindImage(imageBase64 string, searchRegion *model.Region) (int, int, bool, error) {
	templateImage, err := decodeBase64PNG(imageBase64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("decode template image: %w", err)
	}

	screenCapture, captureOriginX, captureOriginY, err := imageDetector.captureScreen(searchRegion)
	if err != nil {
		return 0, 0, false, fmt.Errorf("capture screen: %w", err)
	}

	templateBounds := templateImage.Bounds()
	captureBounds := screenCapture.Bounds()

	templateWidth := templateBounds.Dx()
	templateHeight := templateBounds.Dy()

	for scanY := 0; scanY <= captureBounds.Dy()-templateHeight; scanY++ {
		for scanX := 0; scanX <= captureBounds.Dx()-templateWidth; scanX++ {
			if imageDetector.pixelsMatch(screenCapture, templateImage, scanX, scanY) {
				centerX := captureOriginX + scanX + templateWidth/2
				centerY := captureOriginY + scanY + templateHeight/2
				return centerX, centerY, true, nil
			}
		}
	}

	return 0, 0, false, nil
}

func (imageDetector *Detector) pixelsMatch(screenCapture, templateImage image.Image, startX, startY int) bool {
	templateBounds := templateImage.Bounds()
	for templateY := templateBounds.Min.Y; templateY < templateBounds.Max.Y; templateY++ {
		for templateX := templateBounds.Min.X; templateX < templateBounds.Max.X; templateX++ {
			screenRed, screenGreen, screenBlue, _ := screenCapture.At(startX+templateX, startY+templateY).RGBA()
			templateRed, templateGreen, templateBlue, _ := templateImage.At(templateX, templateY).RGBA()

			// RGBA returns 16-bit values, convert to 8-bit
			if absDifference(int(screenRed>>8), int(templateRed>>8)) > imageDetector.matchTolerance ||
				absDifference(int(screenGreen>>8), int(templateGreen>>8)) > imageDetector.matchTolerance ||
				absDifference(int(screenBlue>>8), int(templateBlue>>8)) > imageDetector.matchTolerance {
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

func decodeBase64PNG(encodedString string) (image.Image, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(encodedString)
	if err != nil {
		return nil, err
	}
	decodedImage, err := png.Decode(bytes.NewReader(decodedBytes))
	if err != nil {
		return nil, err
	}
	return decodedImage, nil
}

// captureScreen captures the full screen or a region using Win32 GDI.
func (imageDetector *Detector) captureScreen(searchRegion *model.Region) (image.Image, int, int, error) {
	screenDC, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screenDC)

	var captureX, captureY, captureWidth, captureHeight int
	if searchRegion != nil {
		captureX = searchRegion.X
		captureY = searchRegion.Y
		captureWidth = searchRegion.Width
		captureHeight = searchRegion.Height
	} else {
		screenWidthValue, _, _ := procGetSystemMetrics.Call(systemMetricScreenWidth)
		screenHeightValue, _, _ := procGetSystemMetrics.Call(systemMetricScreenHeight)
		captureWidth = int(screenWidthValue)
		captureHeight = int(screenHeightValue)
	}

	memoryDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	defer procDeleteDC.Call(memoryDC)

	bitmapHandle, _, _ := procCreateCompatibleBitmap.Call(screenDC, uintptr(captureWidth), uintptr(captureHeight))
	defer procDeleteObject.Call(bitmapHandle)

	procSelectObject.Call(memoryDC, bitmapHandle)
	procBitBlt.Call(memoryDC, 0, 0, uintptr(captureWidth), uintptr(captureHeight),
		screenDC, uintptr(captureX), uintptr(captureY), bitBltSourceCopy)

	// Read bitmap data
	type bitmapInfoHeader struct {
		BiSize          uint32
		BiWidth         int32
		BiHeight        int32
		BiPlanes        uint16
		BiBitCount      uint16
		BiCompression   uint32
		BiSizeImage     uint32
		BiXPelsPerMeter int32
		BiYPelsPerMeter int32
		BiClrUsed       uint32
		BiClrImportant  uint32
	}

	headerInfo := bitmapInfoHeader{
		BiSize:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:    int32(captureWidth),
		BiHeight:   -int32(captureHeight), // top-down
		BiPlanes:   1,
		BiBitCount: 32,
	}

	pixelDataSize := captureWidth * captureHeight * 4
	pixelBuffer := make([]byte, pixelDataSize)

	procGetDIBits.Call(memoryDC, bitmapHandle, 0, uintptr(captureHeight),
		uintptr(unsafe.Pointer(&pixelBuffer[0])),
		uintptr(unsafe.Pointer(&headerInfo)), 0)

	capturedImage := image.NewRGBA(image.Rect(0, 0, captureWidth, captureHeight))
	for pixelIndex := 0; pixelIndex < captureWidth*captureHeight; pixelIndex++ {
		byteOffset := pixelIndex * 4
		// GDI returns BGRA, convert to RGBA
		capturedImage.Pix[byteOffset+0] = pixelBuffer[byteOffset+2] // R
		capturedImage.Pix[byteOffset+1] = pixelBuffer[byteOffset+1] // G
		capturedImage.Pix[byteOffset+2] = pixelBuffer[byteOffset+0] // B
		capturedImage.Pix[byteOffset+3] = 255                       // A
	}

	return capturedImage, captureX, captureY, nil
}
