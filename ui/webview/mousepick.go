package webview

// mouseTick is one poll of the mouse-capture picker. pressed reports "pressed since the last call"
// (GetAsyncKeyState bit 0, which this read consumes), so a press made while another application is
// foreground is discarded, never delivered later. active gates on the capturing window being
// foreground (editor-window contract §8); nil means ungated.
func mouseTick(vk uint32, pressed func(uint32) bool, active func() bool, cursor func() (int, int, bool), onPick func(x, y int)) {
	if vk == 0 || !pressed(vk) {
		return
	}
	if active != nil && !active() {
		return
	}
	if x, y, ok := cursor(); ok {
		onPick(x, y)
	}
}
