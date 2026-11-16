//go:build windows

package image

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/yanmad27/supcua-v2/model"
)

var (
	user32DLL                  = syscall.NewLazyDLL("user32.dll")
	gdi32DLL                   = syscall.NewLazyDLL("gdi32.dll")
	procGetDC                  = user32DLL.NewProc("GetDC")
	procReleaseDC              = user32DLL.NewProc("ReleaseDC")
	procGetSystemMetrics       = user32DLL.NewProc("GetSystemMetrics")
	procCreateCompatibleDC     = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32DLL.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32DLL.NewProc("SelectObject")
	procBitBlt                 = gdi32DLL.NewProc("BitBlt")
	procDeleteObject           = gdi32DLL.NewProc("DeleteObject")
	procDeleteDC               = gdi32DLL.NewProc("DeleteDC")
	procGetDIBits              = gdi32DLL.NewProc("GetDIBits")
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
	template, err := sharedTemplateCache.get(imageBase64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("decode template image: %w", err)
	}

	screenPix, captureWidth, captureHeight, captureOriginX, captureOriginY, err := captureScreen(searchRegion)
	if err != nil {
		return 0, 0, false, fmt.Errorf("capture screen: %w", err)
	}
	defer releaseCaptureBuffer(screenPix)

	scanX, scanY, found := findMatch(screenPix, captureWidth, captureHeight, template, imageDetector.matchTolerance)
	if !found {
		return 0, 0, false, nil
	}
	return captureOriginX + scanX + template.width/2, captureOriginY + scanY + template.height/2, true, nil
}

// captureScreen captures the full screen or a region using Win32 GDI into a
// pooled RGBA buffer; the caller must releaseCaptureBuffer it.
func captureScreen(searchRegion *model.Region) ([]byte, int, int, int, int, error) {
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
	if err := validateSearchSize(captureWidth, captureHeight); err != nil {
		return nil, 0, 0, 0, 0, err
	}

	screenDC, _, getDCErr := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, 0, 0, 0, 0, fmt.Errorf("GetDC failed: %v", getDCErr)
	}
	defer procReleaseDC.Call(0, screenDC)

	memoryDC, _, createDCErr := procCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return nil, 0, 0, 0, 0, fmt.Errorf("CreateCompatibleDC failed: %v", createDCErr)
	}
	defer procDeleteDC.Call(memoryDC)

	bitmapHandle, _, createBitmapErr := procCreateCompatibleBitmap.Call(screenDC, uintptr(captureWidth), uintptr(captureHeight))
	if bitmapHandle == 0 {
		return nil, 0, 0, 0, 0, fmt.Errorf("CreateCompatibleBitmap failed: %v", createBitmapErr)
	}
	// Deferred calls run LIFO: the bitmap is deleted before the DC is deleted.
	defer procDeleteObject.Call(bitmapHandle)

	previousObject, _, selectErr := procSelectObject.Call(memoryDC, bitmapHandle)
	if previousObject == 0 {
		return nil, 0, 0, 0, 0, fmt.Errorf("SelectObject failed: %v", selectErr)
	}
	// Runs before DeleteObject(bitmap) (a selected bitmap cannot be deleted).
	bitmapSelected := true
	deselectBitmap := func() {
		if bitmapSelected {
			procSelectObject.Call(memoryDC, previousObject)
			bitmapSelected = false
		}
	}
	defer deselectBitmap()

	blitResult, _, blitErr := procBitBlt.Call(memoryDC, 0, 0, uintptr(captureWidth), uintptr(captureHeight),
		screenDC, uintptr(captureX), uintptr(captureY), bitBltSourceCopy)
	if blitResult == 0 {
		return nil, 0, 0, 0, 0, fmt.Errorf("BitBlt failed: %v", blitErr)
	}

	// GetDIBits requires the bitmap not be selected into a DC.
	deselectBitmap()

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

	pixelBuffer := acquireCaptureBuffer(captureWidth * captureHeight * 4)
	scanLines, _, dibErr := procGetDIBits.Call(memoryDC, bitmapHandle, 0, uintptr(captureHeight),
		uintptr(unsafe.Pointer(&pixelBuffer[0])),
		uintptr(unsafe.Pointer(&headerInfo)), 0)
	if scanLines == 0 {
		releaseCaptureBuffer(pixelBuffer)
		return nil, 0, 0, 0, 0, fmt.Errorf("GetDIBits failed: %v", dibErr)
	}

	bgraToRGBA(pixelBuffer)
	return pixelBuffer, captureWidth, captureHeight, captureX, captureY, nil
}
