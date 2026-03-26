//go:build !windows

package ui

type OverlayWindow struct {
	isVisible bool
}

func (overlayWindow *OverlayWindow) Refresh()                    {}
func (overlayWindow *OverlayWindow) ToggleVisibility()           { overlayWindow.isVisible = !overlayWindow.isVisible }
func (overlayWindow *OverlayWindow) IsVisible() bool             { return overlayWindow.isVisible }
func (overlayWindow *OverlayWindow) GetPosition() (int, int)     { return 0, 0 }
func (overlayWindow *OverlayWindow) Destroy()                    {}
