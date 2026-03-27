//go:build windows

package ui

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/capture"
	"github.com/yanmad27/supcua-v2/model"
	"github.com/yanmad27/supcua-v2/storage"
)

// scriptEditController holds the state for the script editor dialog.
type scriptEditController struct {
	dialog      *walk.Dialog
	script      *model.Script
	scriptStore *storage.ScriptStore
	saved       bool

	// Fields
	nameEdit              *walk.LineEdit
	enabledCheckbox       *walk.CheckBox
	runModeOnceRadio      *walk.RadioButton
	runModeInfiniteRadio  *walk.RadioButton
	hotkeyModeToggleRadio *walk.RadioButton
	hotkeyModeSeparateRadio *walk.RadioButton
	actionListBox         *walk.ListBox

	// Hotkey fields
	toggleHotkeyEdit    *walk.LineEdit
	toggleHotkeyGroup   *walk.GroupBox
	startHotkeyEdit     *walk.LineEdit
	stopHotkeyEdit      *walk.LineEdit
	separateHotkeyGroup *walk.GroupBox

	// Mouse fields
	mouseXLabel *walk.Label
	mouseYLabel *walk.Label

	// Keyboard field
	keyEdit *walk.LineEdit

	// Wait field
	delayMsEdit *walk.NumberEdit

	// Loop field
	loopCountEdit *walk.NumberEdit
}

// ShowScriptEditDialog opens a dialog to create or edit a script.
// Returns true if the script was saved.
func ShowScriptEditDialog(owner walk.Form, script *model.Script, store *storage.ScriptStore, configStore *storage.ConfigStore) bool {
	ctrl := &scriptEditController{
		script:      script,
		scriptStore: store,
	}

	title := "Edit Script"
	if script.Name == "New Script" && len(script.Actions) == 0 {
		title = "New Script"
	}

	dlgDef := Dialog{
		AssignTo: &ctrl.dialog,
		Title:    title,
		MinSize:  Size{Width: 550, Height: 490},
		Size:     Size{Width: 630, Height: 540},
		Layout:   VBox{MarginsZero: true},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, SpacingZero: true},
				Children: []Widget{
					// Left side: properties + hotkeys + add action buttons
					Composite{
						Layout:  VBox{Margins: Margins{Left: 4, Top: 4, Right: 0, Bottom: 4}, Spacing: 2},
						MaxSize: Size{Width: 255},
						Children: []Widget{
							GroupBox{
								Title:  "Script Properties",
								Layout: Grid{Columns: 2},
								Children: []Widget{
									Label{Text: "Name:"},
									LineEdit{AssignTo: &ctrl.nameEdit},
									Label{Text: "Enabled:"},
									CheckBox{AssignTo: &ctrl.enabledCheckbox},
									Label{Text: "Run Mode:"},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											RadioButton{
												AssignTo: &ctrl.runModeOnceRadio,
												Text:     "Once",
											},
											RadioButton{
												AssignTo: &ctrl.runModeInfiniteRadio,
												Text:     "Infinite",
											},
										},
									},
									Label{Text: "Hotkey Mode:"},
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											RadioButton{
												AssignTo: &ctrl.hotkeyModeToggleRadio,
												Text:     "Toggle",
												OnClicked: func() {
													ctrl.onHotkeyModeChanged()
												},
											},
											RadioButton{
												AssignTo: &ctrl.hotkeyModeSeparateRadio,
												Text:     "Separate",
												OnClicked: func() {
													ctrl.onHotkeyModeChanged()
												},
											},
										},
									},
								},
							},
							GroupBox{
								AssignTo: &ctrl.toggleHotkeyGroup,
								Title:    "Toggle Hotkey (click & press keys)",
								Layout:   HBox{},
								Children: []Widget{
									LineEdit{
										AssignTo: &ctrl.toggleHotkeyEdit,
										ReadOnly: true,
									},
									PushButton{
										Text:    "Clear",
										MaxSize: Size{Width: 55},
										OnClicked: func() {
											ctrl.script.ToggleHotkey = nil
											ctrl.toggleHotkeyEdit.SetText("(none)")
										},
									},
								},
							},
							GroupBox{
								AssignTo: &ctrl.separateHotkeyGroup,
								Title:    "Start / Stop Hotkeys (click & press keys)",
								Layout:   Grid{Columns: 3},
								Children: []Widget{
									Label{Text: "Start:"},
									LineEdit{
										AssignTo: &ctrl.startHotkeyEdit,
										ReadOnly: true,
									},
									PushButton{
										Text:    "Clear",
										MaxSize: Size{Width: 55},
										OnClicked: func() {
											ctrl.script.StartHotkey = nil
											ctrl.startHotkeyEdit.SetText("(none)")
										},
									},
									Label{Text: "Stop:"},
									LineEdit{
										AssignTo: &ctrl.stopHotkeyEdit,
										ReadOnly: true,
									},
									PushButton{
										Text:    "Clear",
										MaxSize: Size{Width: 55},
										OnClicked: func() {
											ctrl.script.StopHotkey = nil
											ctrl.stopHotkeyEdit.SetText("(none)")
										},
									},
								},
							},
							GroupBox{
								Title:  "Mouse",
								Layout: VBox{Margins: Margins{Left: 4, Top: 2, Right: 4, Bottom: 4}, Spacing: 2},
								Children: []Widget{
								Composite{
									Layout: HBox{MarginsZero: true},
									Children: []Widget{
										Label{Text: "X:"},
										Label{AssignTo: &ctrl.mouseXLabel, Text: "0", MinSize: Size{Width: 35}},
										Label{Text: "Y:"},
										Label{AssignTo: &ctrl.mouseYLabel, Text: "0", MinSize: Size{Width: 35}},
										HSpacer{},
										PushButton{
											Text:      "Move",
											OnClicked: func() { ctrl.addMouseMove() },
										},
									},
								},
								Composite{
									Layout: Grid{Columns: 2, MarginsZero: true},
									Children: []Widget{
										PushButton{Text: "Left Click",   OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseLeftClick) }},
										PushButton{Text: "Right Click",  OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseRightClick) }},
										PushButton{Text: "Left Double",  OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseLeftDouble) }},
										PushButton{Text: "Right Double", OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseRightDouble) }},
										PushButton{Text: "Left Down",    OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseLeftDown) }},
										PushButton{Text: "Right Down",   OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseRightDown) }},
										PushButton{Text: "Left Up",      OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseLeftUp) }},
										PushButton{Text: "Right Up",     OnClicked: func() { ctrl.addSimpleAction(model.ActionMouseRightUp) }},
									},
								},
							},
							},
							GroupBox{
								Title:  "Keyboard",
								Layout: VBox{Margins: Margins{Left: 4, Top: 2, Right: 4, Bottom: 4}, Spacing: 2},
								Children: []Widget{
									Composite{
										Layout: Grid{Columns: 2, MarginsZero: true},
										Children: []Widget{
											LineEdit{AssignTo: &ctrl.keyEdit, ReadOnly: true},
											PushButton{Text: "Press", OnClicked: func() { ctrl.addKeyPress() }},
										},
									},
									Composite{
										Layout: Grid{Columns: 2, MarginsZero: true},
										Children: []Widget{
											PushButton{Text: "Key Down", OnClicked: func() { ctrl.addKeyAction(model.ActionKeyDown) }},
											PushButton{Text: "Key Up",   OnClicked: func() { ctrl.addKeyAction(model.ActionKeyUp) }},
										},
									},
								},
							},
							GroupBox{
								Title:  "Wait",
								Layout: VBox{Margins: Margins{Left: 4, Top: 2, Right: 4, Bottom: 4}, Spacing: 2},
								Children: []Widget{
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											NumberEdit{AssignTo: &ctrl.delayMsEdit, MinValue: 0, MaxValue: 999999, Value: 100},
											PushButton{Text: "Wait", OnClicked: func() { ctrl.addDelay() }},
										},
									},
								},
							},
							GroupBox{
								Title:  "Loop",
								Layout: VBox{Margins: Margins{Left: 4, Top: 2, Right: 4, Bottom: 4}, Spacing: 2},
								Children: []Widget{
									Composite{
										Layout: HBox{MarginsZero: true},
										Children: []Widget{
											NumberEdit{
												AssignTo:    &ctrl.loopCountEdit,
												MinValue:    0,
												MaxValue:    9999,
												Value:       1,
												ToolTipText: "0 = infinite",
											},
											PushButton{
												Text:      "Start Loop",
												OnClicked: func() { ctrl.addLoop() },
											},
											PushButton{
												Text:      "End Loop",
												OnClicked: func() { ctrl.addSimpleAction("loop_end") },
											},
										},
									},
								},
							},
							VSpacer{},
						},
					},
					// Right side: action list
					Composite{
						Layout: VBox{Margins: Margins{Left: 0, Top: 4, Right: 4, Bottom: 4}},
						MaxSize: Size{Width: 255},
						Children: []Widget{
							Label{Text: "Actions"},
							ListBox{
								AssignTo: &ctrl.actionListBox,
								MinSize:  Size{Height: 300},
							},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									PushButton{
										MaxSize:   Size{Width: 60},
										Text:      "Edit",
										OnClicked: func() { ctrl.onEditAction() },
									},
									PushButton{
										Text:      "Delete",
										OnClicked: func() { ctrl.onDeleteAction() },
										MaxSize:   Size{Width: 60},
									},
									PushButton{
										Text:      "Up",
										MaxSize:   Size{Width: 50},
										OnClicked: func() { ctrl.onMoveActionUp() },
									},
									PushButton{
										Text:      "Down",
										OnClicked: func() { ctrl.onMoveActionDown() },
									},
									HSpacer{},
								},
							},
						},
					},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text:      "Save",
						OnClicked: func() { ctrl.onSave() },
					},
					PushButton{
						Text:      "Cancel",
						OnClicked: func() { ctrl.dialog.Cancel() },
					},
				},
			},
		},
	}

	if err := dlgDef.Create(owner); err != nil {
		walk.MsgBox(owner, "Error", "Failed to open editor: "+err.Error(), walk.MsgBoxIconError)
		return false
	}

	// Restore saved position
	if configStore != nil {
		cfg, err := configStore.Load()
		if err == nil && (cfg.EditDialogPos.X != 0 || cfg.EditDialogPos.Y != 0) {
			b := ctrl.dialog.Bounds()
			ctrl.dialog.SetBounds(walk.Rectangle{X: cfg.EditDialogPos.X, Y: cfg.EditDialogPos.Y, Width: b.Width, Height: b.Height})
		}
		// Save position on close
		ctrl.dialog.Closing().Attach(func(cancelled *bool, reason walk.CloseReason) {
			cfg, err := configStore.Load()
			if err == nil {
				b := ctrl.dialog.Bounds()
				cfg.EditDialogPos = model.WindowPos{X: b.X, Y: b.Y}
				configStore.Save(cfg)
			}
		})
	}

	ctrl.loadFields()
	ctrl.dialog.Run()

	return ctrl.saved
}

func (ctrl *scriptEditController) loadFields() {
	// Make read-only hotkey fields look white like editable fields
	white, _ := walk.NewSolidColorBrush(walk.RGB(255, 255, 255))
	ctrl.toggleHotkeyEdit.SetBackground(white)
	ctrl.startHotkeyEdit.SetBackground(white)
	ctrl.stopHotkeyEdit.SetBackground(white)
	ctrl.keyEdit.SetBackground(white)

	ctrl.nameEdit.SetText(ctrl.script.Name)
	ctrl.enabledCheckbox.SetChecked(ctrl.script.Enabled)

	if ctrl.script.RunMode == model.RunModeInfinite {
		ctrl.runModeInfiniteRadio.SetChecked(true)
	} else {
		ctrl.runModeOnceRadio.SetChecked(true)
	}

	if ctrl.script.HotkeyMode == model.HotkeyModeSeparate {
		ctrl.hotkeyModeSeparateRadio.SetChecked(true)
	} else {
		ctrl.hotkeyModeToggleRadio.SetChecked(true)
	}

	// Load hotkey display values
	ctrl.toggleHotkeyEdit.SetText(formatHotkeyDisplay(ctrl.script.ToggleHotkey))
	ctrl.startHotkeyEdit.SetText(formatHotkeyDisplay(ctrl.script.StartHotkey))
	ctrl.stopHotkeyEdit.SetText(formatHotkeyDisplay(ctrl.script.StopHotkey))

	// Register global P hotkey to capture mouse position into X/Y
	ctrl.registerPickHotkey()

	// Attach key capture to keyboard input field
	ctrl.keyEdit.KeyDown().Attach(func(key walk.Key) {
		keyName := keyToString(key)
		if keyName != "" && keyName != "ctrl" && keyName != "shift" && keyName != "alt" {
			ctrl.keyEdit.SetText(keyName)
		}
	})

	// Attach key capture handlers to hotkey fields
	attachHotkeyCapture(ctrl.toggleHotkeyEdit, &ctrl.script.ToggleHotkey)
	attachHotkeyCapture(ctrl.startHotkeyEdit, &ctrl.script.StartHotkey)
	attachHotkeyCapture(ctrl.stopHotkeyEdit, &ctrl.script.StopHotkey)

	ctrl.onHotkeyModeChanged()
	ctrl.refreshActionList()
}

var (
	user32Pick           = syscall.NewLazyDLL("user32.dll")
	procGetAsyncKeyState = user32Pick.NewProc("GetAsyncKeyState")
)

const pickHotkeyID = 0x7F00 // unique ID for pick hotkey

func (ctrl *scriptEditController) registerPickHotkey() {
	// Use GetAsyncKeyState polling since RegisterHotKey requires message loop on same thread
	stopCh := make(chan struct{})

	go func() {
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			ret, _, _ := procGetAsyncKeyState.Call(uintptr(0x50)) // 0x50 = 'P'
			// Bit 0 means key was pressed since last call
			if ret&1 != 0 {
				x, y, err := capture.GetCurrentMousePosition()
				if err == nil {
					ctrl.dialog.Synchronize(func() {
						// Don't capture if name or key input is focused
						if ctrl.nameEdit.Focused() || ctrl.keyEdit.Focused() {
							return
						}
						ctrl.mouseXLabel.SetText(strconv.Itoa(x))
						ctrl.mouseYLabel.SetText(strconv.Itoa(y))
					})
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	ctrl.dialog.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		close(stopCh)
	})
}

func attachHotkeyCapture(edit *walk.LineEdit, target **model.Hotkey) {
	edit.KeyDown().Attach(func(key walk.Key) {
		keyName := keyToString(key)
		if keyName == "" {
			return
		}
		// Ignore bare modifier keys — wait for the actual key
		if keyName == "ctrl" || keyName == "shift" || keyName == "alt" {
			return
		}

		modifiers := []string{}
		if walk.ControlDown() {
			modifiers = append(modifiers, "ctrl")
		}
		if walk.ShiftDown() {
			modifiers = append(modifiers, "shift")
		}
		if walk.AltDown() {
			modifiers = append(modifiers, "alt")
		}

		hk := &model.Hotkey{Modifiers: modifiers, Key: keyName}
		*target = hk
		edit.SetText(formatHotkeyDisplay(hk))
	})
}

func keyToString(key walk.Key) string {
	switch {
	case key >= walk.KeyA && key <= walk.KeyZ:
		return string(rune('a' + (key - walk.KeyA)))
	case key >= walk.Key0 && key <= walk.Key9:
		return string(rune('0' + (key - walk.Key0)))
	case key >= walk.KeyF1 && key <= walk.KeyF24:
		return fmt.Sprintf("f%d", key-walk.KeyF1+1)
	case key == walk.KeySpace:
		return "space"
	case key == walk.KeyReturn:
		return "enter"
	case key == walk.KeyEscape:
		return "escape"
	case key == walk.KeyTab:
		return "tab"
	case key == walk.KeyBack:
		return "backspace"
	case key == walk.KeyDelete:
		return "delete"
	case key == walk.KeyInsert:
		return "insert"
	case key == walk.KeyHome:
		return "home"
	case key == walk.KeyEnd:
		return "end"
	case key == walk.KeyPrior:
		return "pageup"
	case key == walk.KeyNext:
		return "pagedown"
	case key == walk.KeyUp:
		return "up"
	case key == walk.KeyDown:
		return "down"
	case key == walk.KeyLeft:
		return "left"
	case key == walk.KeyRight:
		return "right"
	case key == walk.KeyControl:
		return "ctrl"
	case key == walk.KeyShift:
		return "shift"
	case key == walk.KeyMenu:
		return "alt"
	default:
		return fmt.Sprintf("0x%02X", int(key))
	}
}

func (ctrl *scriptEditController) onHotkeyModeChanged() {
	if ctrl.toggleHotkeyGroup == nil || ctrl.separateHotkeyGroup == nil {
		return
	}
	isToggle := ctrl.hotkeyModeToggleRadio.Checked()
	ctrl.toggleHotkeyGroup.SetVisible(isToggle)
	ctrl.separateHotkeyGroup.SetVisible(!isToggle)
}


func formatHotkeyDisplay(hk *model.Hotkey) string {
	if hk == nil {
		return "(none)"
	}
	parts := make([]string, 0, len(hk.Modifiers)+1)
	for _, mod := range hk.Modifiers {
		parts = append(parts, strings.ToUpper(mod[:1])+mod[1:])
	}
	parts = append(parts, strings.ToUpper(hk.Key))
	return strings.Join(parts, " + ")
}

func (ctrl *scriptEditController) refreshActionList() {
	descriptions := make([]string, len(ctrl.script.Actions))
	for i, action := range ctrl.script.Actions {
		descriptions[i] = describeAction(action)
	}
	ctrl.actionListBox.SetModel(descriptions)
}

func (ctrl *scriptEditController) readFieldsIntoScript() {
	ctrl.script.Name = ctrl.nameEdit.Text()
	ctrl.script.Enabled = ctrl.enabledCheckbox.Checked()

	if ctrl.runModeInfiniteRadio.Checked() {
		ctrl.script.RunMode = model.RunModeInfinite
	} else {
		ctrl.script.RunMode = model.RunModeOnce
	}

	if ctrl.hotkeyModeSeparateRadio.Checked() {
		ctrl.script.HotkeyMode = model.HotkeyModeSeparate
	} else {
		ctrl.script.HotkeyMode = model.HotkeyModeToggle
	}
}

func (ctrl *scriptEditController) onSave() {
	ctrl.readFieldsIntoScript()

	err := ctrl.scriptStore.Save(*ctrl.script)
	if err != nil {
		walk.MsgBox(ctrl.dialog, "Error", "Failed to save: "+err.Error(), walk.MsgBoxIconError)
		return
	}
	ctrl.saved = true
	ctrl.dialog.Accept()
}

func (ctrl *scriptEditController) addMouseMove() {
	x, _ := strconv.Atoi(ctrl.mouseXLabel.Text())
	y, _ := strconv.Atoi(ctrl.mouseYLabel.Text())
	ctrl.script.Actions = append(ctrl.script.Actions, model.Action{
		Type: model.ActionMouseMove,
		X:    x,
		Y:    y,
	})
	ctrl.refreshActionList()
}



func (ctrl *scriptEditController) addKeyPress() {
	key := ctrl.keyEdit.Text()
	if key == "" {
		return
	}
	ctrl.script.Actions = append(ctrl.script.Actions,
		model.Action{Type: model.ActionKeyPress, Key: key},
	)
	ctrl.refreshActionList()
}

func (ctrl *scriptEditController) addKeyAction(actionType string) {
	key := ctrl.keyEdit.Text()
	if key == "" {
		return
	}
	ctrl.script.Actions = append(ctrl.script.Actions, model.Action{Type: actionType, Key: key})
	ctrl.refreshActionList()
}

func (ctrl *scriptEditController) addDelay() {
	ms := int(ctrl.delayMsEdit.Value())
	if ms <= 0 {
		return
	}
	ctrl.script.Actions = append(ctrl.script.Actions, model.Action{Type: model.ActionDelay, DelayMs: ms})
	ctrl.refreshActionList()
}

func (ctrl *scriptEditController) addLoop() {
	count := int(ctrl.loopCountEdit.Value())
	ctrl.script.Actions = append(ctrl.script.Actions, model.Action{Type: model.ActionLoop, LoopCount: count})
	ctrl.refreshActionList()
}

func (ctrl *scriptEditController) addSimpleAction(actionType string) {
	ctrl.script.Actions = append(ctrl.script.Actions, model.Action{Type: actionType})
	ctrl.refreshActionList()
}

func (ctrl *scriptEditController) addActionOfType(actionType string) {
	newAction := model.Action{Type: actionType}
	editedAction, accepted := ShowActionParamDialog(ctrl.dialog, &newAction)
	if !accepted {
		return
	}
	ctrl.script.Actions = append(ctrl.script.Actions, editedAction)
	ctrl.refreshActionList()
}

func (ctrl *scriptEditController) onEditAction() {
	selectedIndex := ctrl.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(ctrl.script.Actions) {
		return
	}
	existingAction := &ctrl.script.Actions[selectedIndex]

	switch existingAction.Type {
	case model.ActionMouseLeftClick, model.ActionMouseRightClick,
		model.ActionMouseLeftDouble, model.ActionMouseRightDouble,
		model.ActionMouseLeftDown, model.ActionMouseLeftUp,
		model.ActionMouseRightDown, model.ActionMouseRightUp:
		return
	}

	editedAction, accepted := ShowActionParamDialog(ctrl.dialog, existingAction)
	if !accepted {
		return
	}
	ctrl.script.Actions[selectedIndex] = editedAction
	ctrl.refreshActionList()
}

func (ctrl *scriptEditController) onDeleteAction() {
	selectedIndex := ctrl.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(ctrl.script.Actions) {
		return
	}
	ctrl.script.Actions = append(
		ctrl.script.Actions[:selectedIndex],
		ctrl.script.Actions[selectedIndex+1:]...,
	)
	ctrl.refreshActionList()
	// Reselect nearest item
	count := len(ctrl.script.Actions)
	if count > 0 {
		if selectedIndex >= count {
			selectedIndex = count - 1
		}
		ctrl.actionListBox.SetCurrentIndex(selectedIndex)
	}
}

func (ctrl *scriptEditController) onMoveActionUp() {
	selectedIndex := ctrl.actionListBox.CurrentIndex()
	if selectedIndex <= 0 {
		return
	}
	actions := ctrl.script.Actions
	actions[selectedIndex], actions[selectedIndex-1] = actions[selectedIndex-1], actions[selectedIndex]
	ctrl.refreshActionList()
	ctrl.actionListBox.SetCurrentIndex(selectedIndex - 1)
}

func (ctrl *scriptEditController) onMoveActionDown() {
	selectedIndex := ctrl.actionListBox.CurrentIndex()
	if selectedIndex < 0 || selectedIndex >= len(ctrl.script.Actions)-1 {
		return
	}
	actions := ctrl.script.Actions
	actions[selectedIndex], actions[selectedIndex+1] = actions[selectedIndex+1], actions[selectedIndex]
	ctrl.refreshActionList()
	ctrl.actionListBox.SetCurrentIndex(selectedIndex + 1)
}

func describeAction(action model.Action) string {
	switch action.Type {
	case model.ActionMouseMove:
		if action.Source == "found" {
			return "Mouse Move -> found position"
		}
		return fmt.Sprintf("Mouse Move -> (%d, %d)", action.X, action.Y)
	case model.ActionMouseLeftClick:
		return "Mouse Left Click"
	case model.ActionMouseRightClick:
		return "Mouse Right Click"
	case model.ActionMouseLeftDouble:
		return "Mouse Left Double Click"
	case model.ActionMouseRightDouble:
		return "Mouse Right Double Click"
	case model.ActionMouseLeftDown:
		return "Mouse Left Down"
	case model.ActionMouseLeftUp:
		return "Mouse Left Up"
	case model.ActionMouseRightDown:
		return "Mouse Right Down"
	case model.ActionMouseRightUp:
		return "Mouse Right Up"
	case model.ActionKeyPress:
		return "Key Press: " + action.Key
	case model.ActionKeyDown:
		return "Key Down: " + action.Key
	case model.ActionKeyUp:
		return "Key Up: " + action.Key
	case model.ActionDelay:
		return fmt.Sprintf("Delay: %dms", action.DelayMs)
	case model.ActionLoop:
		if action.LoopCount == 0 {
			return "Loop (infinite)"
		}
		return fmt.Sprintf("Loop (%dx)", action.LoopCount)
	case "loop_end":
		return "End Loop"
	case model.ActionWaitForImage:
		return fmt.Sprintf("Wait For Image (timeout: %dms)", action.TimeoutMs)
	case model.ActionIfImage:
		return fmt.Sprintf("If Image (then: %d, else: %d)", len(action.ThenActions), len(action.ElseActions))
	default:
		return action.Type
	}
}
